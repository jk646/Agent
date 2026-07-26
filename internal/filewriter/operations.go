package filewriter

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/example/agent-shell-tool/internal/writepolicy"
)

type preparedWrite struct {
	operation                              Operation
	path                                   ResolvedPath
	before, after                          []byte
	exists                                 bool
	mode                                   os.FileMode
	beforeSHA, afterSHA, action, temporary string
}

var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func (m *Manager) Create(ctx context.Context, params Operation) (Result, error) {
	params.Kind = "create"
	return m.Batch(ctx, BatchParams{WriteID: params.WriteID, Operations: []Operation{params}})
}
func (m *Manager) Overwrite(ctx context.Context, params Operation) (Result, error) {
	params.Kind = "overwrite"
	return m.Batch(ctx, BatchParams{WriteID: params.WriteID, Operations: []Operation{params}})
}
func (m *Manager) Append(ctx context.Context, params Operation) (Result, error) {
	params.Kind = "append"
	return m.Batch(ctx, BatchParams{WriteID: params.WriteID, Operations: []Operation{params}})
}
func (m *Manager) WriteAt(ctx context.Context, params Operation) (Result, error) {
	params.Kind = "write_at"
	return m.Batch(ctx, BatchParams{WriteID: params.WriteID, Operations: []Operation{params}})
}
func (m *Manager) Preview(ctx context.Context, params BatchParams) (Result, error) {
	params.Preview = true
	return m.Batch(ctx, params)
}

func (m *Manager) Batch(parent context.Context, params BatchParams) (Result, error) {
	writeID, ctx, release, err := m.begin(parent, params.WriteID)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if len(params.Operations) == 0 || len(params.Operations) > m.cfg.MaxBatchFiles {
		return Result{}, fmt.Errorf("%w: operations must contain 1 to %d items", ErrInvalidRequest, m.cfg.MaxBatchFiles)
	}
	resolved := make([]ResolvedPath, len(params.Operations))
	paths := make([]string, len(params.Operations))
	seen := make(map[string]struct{})
	for index, operation := range params.Operations {
		path, err := m.resolver.Resolve(operation.Path)
		if err != nil {
			return Result{}, err
		}
		if _, exists := seen[path.Relative]; exists {
			return Result{}, fmt.Errorf("%w: duplicate path %s", ErrInvalidRequest, path.Relative)
		}
		seen[path.Relative] = struct{}{}
		resolved[index] = path
		paths[index] = path.Relative
	}
	unlock := m.locks.lock(paths)
	defer unlock()
	prepared := make([]preparedWrite, 0, len(params.Operations))
	var total int64
	for index, operation := range params.Operations {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if err := m.policy.Authorize(ctx, writepolicy.Operation{Kind: operation.Kind, Path: resolved[index].Relative}); err != nil {
			return Result{}, err
		}
		item, err := m.prepare(operation, resolved[index])
		if err != nil {
			return Result{}, err
		}
		total += int64(len(item.after))
		if total > m.cfg.MaxBatchBytes {
			return Result{}, ErrTooLarge
		}
		prepared = append(prepared, item)
	}
	changes := changesFor(prepared, params.Preview)
	if params.Preview {
		return Result{WriteID: writeID, Applied: false, Preview: true, Files: changes}, nil
	}
	manifest, journalDir, err := m.journal.create(m.resolver.Root(), prepared)
	if err != nil {
		return Result{}, err
	}
	committed := 0
	defer func() {
		for _, item := range prepared {
			if item.temporary != "" {
				_ = os.Remove(item.temporary)
			}
		}
	}()
	for index := range prepared {
		if err := ctx.Err(); err != nil {
			_ = m.restoreUnchecked(journalDir, manifest, committed)
			return Result{}, err
		}
		if prepared[index].operation.CreateParents {
			if err := os.MkdirAll(filepath.Dir(prepared[index].path.Absolute), 0o755); err != nil {
				_ = m.restoreUnchecked(journalDir, manifest, committed)
				return Result{}, err
			}
		}
		if _, err := m.resolver.Resolve(prepared[index].path.Relative); err != nil {
			_ = m.restoreUnchecked(journalDir, manifest, committed)
			return Result{}, err
		}
		if err := verifyCurrent(prepared[index]); err != nil {
			_ = m.restoreUnchecked(journalDir, manifest, committed)
			return Result{}, err
		}
		temporary, err := prepareAtomic(prepared[index].path.Absolute, prepared[index].after, prepared[index].mode)
		if err != nil {
			_ = m.restoreUnchecked(journalDir, manifest, committed)
			return Result{}, err
		}
		prepared[index].temporary = temporary
		if err := commitAtomic(temporary, prepared[index].path.Absolute); err != nil {
			_ = m.restoreUnchecked(journalDir, manifest, committed)
			return Result{}, err
		}
		prepared[index].temporary = ""
		committed++
	}
	return Result{WriteID: writeID, TransactionID: manifest.ID, Applied: true, RollbackAvailable: true, Files: changes}, nil
}

func verifyCurrent(item preparedWrite) error {
	info, err := os.Lstat(item.path.Absolute)
	if os.IsNotExist(err) {
		if item.exists {
			return ErrStaleFile
		}
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrSymlink
	}
	if !info.Mode().IsRegular() {
		return ErrUnsupportedType
	}
	if !item.exists {
		return ErrAlreadyExists
	}
	digest, err := hashFile(item.path.Absolute)
	if err != nil {
		return err
	}
	if digest != item.beforeSHA {
		return ErrStaleFile
	}
	return nil
}

func (m *Manager) prepare(operation Operation, path ResolvedPath) (preparedWrite, error) {
	switch operation.Kind {
	case "create", "overwrite", "append", "write_at":
	default:
		return preparedWrite{}, fmt.Errorf("%w: unsupported operation %q", ErrInvalidRequest, operation.Kind)
	}
	if operation.Offset < 0 {
		return preparedWrite{}, fmt.Errorf("%w: offset cannot be negative", ErrInvalidRequest)
	}
	payload, err := decodePayload(operation)
	if err != nil {
		return preparedWrite{}, err
	}
	info, statErr := os.Lstat(path.Absolute)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return preparedWrite{}, statErr
	}
	if exists && info.Mode()&os.ModeSymlink != 0 {
		return preparedWrite{}, ErrSymlink
	}
	if exists && !info.Mode().IsRegular() {
		return preparedWrite{}, ErrUnsupportedType
	}
	if operation.Kind == "create" && exists {
		return preparedWrite{}, ErrAlreadyExists
	}
	if operation.Kind != "create" && !exists && !operation.CreateIfMissing {
		return preparedWrite{}, ErrNotFound
	}
	var before []byte
	mode := os.FileMode(0o644)
	if exists {
		if info.Size() > m.cfg.MaxFileBytes {
			return preparedWrite{}, ErrTooLarge
		}
		before, err = os.ReadFile(path.Absolute)
		if err != nil {
			return preparedWrite{}, err
		}
		mode = info.Mode().Perm()
	}
	beforeSHA := ""
	if exists {
		beforeSHA = hashBytes(before)
	}
	if operation.ExpectedSHA256 != "" {
		if !shaPattern.MatchString(operation.ExpectedSHA256) {
			return preparedWrite{}, fmt.Errorf("%w: expected_sha256 is invalid", ErrInvalidRequest)
		}
		if !strings.EqualFold(operation.ExpectedSHA256, beforeSHA) {
			return preparedWrite{}, ErrStaleFile
		}
	}
	if operation.Mode != "" {
		parsed, err := parseMode(operation.Mode)
		if err != nil {
			return preparedWrite{}, err
		}
		mode = parsed
	}
	var after []byte
	switch operation.Kind {
	case "create", "overwrite":
		after = append([]byte(nil), payload...)
	case "append":
		after = append(append([]byte(nil), before...), payload...)
	case "write_at":
		if operation.Offset > int64(len(before)) && !operation.AllowSparse {
			return preparedWrite{}, fmt.Errorf("%w: offset exceeds file size", ErrInvalidRequest)
		}
		length := int(operation.Offset) + len(payload)
		if length < len(before) {
			length = len(before)
		}
		after = make([]byte, length)
		copy(after, before)
		copy(after[int(operation.Offset):], payload)
	}
	if int64(len(after)) > m.cfg.MaxFileBytes {
		return preparedWrite{}, ErrTooLarge
	}
	action := map[string]string{"create": "created", "overwrite": "overwritten", "append": "appended", "write_at": "modified"}[operation.Kind]
	if !exists {
		action = "created"
	}
	return preparedWrite{operation: operation, path: path, before: before, after: after, exists: exists, mode: mode, beforeSHA: beforeSHA, afterSHA: hashBytes(after), action: action}, nil
}

func decodePayload(operation Operation) ([]byte, error) {
	if operation.Content != "" && operation.DataBase64 != "" {
		return nil, fmt.Errorf("%w: content and data_base64 are mutually exclusive", ErrInvalidRequest)
	}
	var data []byte
	if operation.DataBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(operation.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid data_base64", ErrInvalidRequest)
		}
		data = decoded
	} else {
		data = []byte(operation.Content)
	}
	if operation.AddNewline && (len(data) == 0 || data[len(data)-1] != '\n') {
		data = append(data, '\n')
	}
	return data, nil
}

func parseMode(value string) (os.FileMode, error) {
	if len(value) != 3 && len(value) != 4 {
		return 0, fmt.Errorf("%w: mode must be octal", ErrInvalidRequest)
	}
	parsed, err := strconv.ParseUint(value, 8, 12)
	if err != nil || parsed > 0o666 {
		return 0, fmt.Errorf("%w: mode must not exceed 0666", ErrInvalidRequest)
	}
	return os.FileMode(parsed), nil
}

func changesFor(items []preparedWrite, preview bool) []FileChange {
	result := make([]FileChange, 0, len(items))
	for _, item := range items {
		change := FileChange{Path: item.path.Relative, Action: item.action, Size: int64(len(item.after)), BeforeSHA256: item.beforeSHA, AfterSHA256: item.afterSHA}
		if preview {
			change.Diff, change.DiffTruncated = textDiff(item.path.Relative, item.before, item.after, 64<<10)
		}
		result = append(result, change)
	}
	return result
}
func textDiff(path string, before, after []byte, limit int) (string, bool) {
	if !utf8.Valid(before) || !utf8.Valid(after) || bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 {
		return "", false
	}
	beforeText := strings.TrimSuffix(string(before), "\n")
	afterText := strings.TrimSuffix(string(after), "\n")
	if beforeText != "" {
		beforeText = "-" + strings.ReplaceAll(beforeText, "\n", "\n-")
	}
	if afterText != "" {
		afterText = "+" + strings.ReplaceAll(afterText, "\n", "\n+")
	}
	value := fmt.Sprintf("--- a/%s\n+++ b/%s\n@@ full file @@\n%s\n%s\n", path, path, beforeText, afterText)
	if len(value) > limit {
		return value[:limit], true
	}
	return value, false
}
