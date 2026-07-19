package filetool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func (m *Manager) ApplyEdits(ctx context.Context, params ApplyEditsParams) (TransactionResult, error) {
	return m.Batch(ctx, BatchParams{DryRun: params.DryRun, Permanent: params.Permanent, Operations: params.Changes})
}

func (m *Manager) Batch(ctx context.Context, params BatchParams) (TransactionResult, error) {
	releaseSlot, err := m.acquireSlot()
	if err != nil {
		return TransactionResult{}, err
	}
	defer releaseSlot()
	operations, affected, stagePaths, err := m.normalizeOperations(ctx, params.Operations)
	if err != nil {
		return TransactionResult{}, err
	}
	lockPaths := append(append([]string{}, affected...), stagePaths...)
	releaseLocks := m.locks.acquire(lockPaths)
	defer releaseLocks()
	manifest, transactionDirectory, err := m.journal.create(m.resolver.Root(), affected)
	if err != nil {
		return TransactionResult{}, err
	}
	if params.DryRun {
		defer m.journal.remove(transactionDirectory)
		stageRoot := filepath.Join(transactionDirectory, "stage")
		if err := prepareStage(m.resolver.Root(), stageRoot, stagePaths, m.cfg); err != nil {
			return TransactionResult{}, err
		}
		if err := applyOperations(stageRoot, operations, m.cfg); err != nil {
			return TransactionResult{}, err
		}
		if err := m.journal.finalize(transactionDirectory, stageRoot, manifest); err != nil {
			return TransactionResult{}, err
		}
		return buildTransactionResult(manifest, transactionDirectory, stageRoot, false, false, m.cfg.MaxDiffBytes)
	}
	if err := applyOperations(m.resolver.Root(), operations, m.cfg); err != nil {
		_ = m.journal.restore(m.resolver.Root(), transactionDirectory, manifest)
		m.journal.remove(transactionDirectory)
		return TransactionResult{}, err
	}
	if err := m.journal.finalize(transactionDirectory, m.resolver.Root(), manifest); err != nil {
		_ = m.journal.restore(m.resolver.Root(), transactionDirectory, manifest)
		m.journal.remove(transactionDirectory)
		return TransactionResult{}, err
	}
	result, err := buildTransactionResult(manifest, transactionDirectory, m.resolver.Root(), true, !params.Permanent, m.cfg.MaxDiffBytes)
	if err != nil {
		return TransactionResult{}, err
	}
	if params.Permanent {
		m.journal.remove(transactionDirectory)
	}
	return result, nil
}

func (m *Manager) Rollback(ctx context.Context, params RollbackParams) (RollbackResult, error) {
	releaseSlot, err := m.acquireSlot()
	if err != nil {
		return RollbackResult{}, err
	}
	defer releaseSlot()
	manifest, directory, err := m.journal.load(params.TransactionID)
	if err != nil {
		return RollbackResult{}, err
	}
	if manifest.RolledBack {
		return RollbackResult{}, fmt.Errorf("%w: transaction was already rolled back", ErrRollbackConflict)
	}
	paths := make([]string, 0, len(manifest.Records))
	for _, record := range manifest.Records {
		paths = append(paths, record.Path)
		if err := m.policy.Authorize(ctx, fileOperation("rollback", record.Path)); err != nil {
			return RollbackResult{}, err
		}
	}
	releaseLocks := m.locks.acquire(paths)
	defer releaseLocks()
	for _, record := range manifest.Records {
		state, err := inspectPath(filepath.Join(m.resolver.Root(), filepath.FromSlash(record.Path)))
		if err != nil {
			return RollbackResult{}, err
		}
		if state.Exists != record.AfterExists || state.SHA256 != record.AfterSHA256 || state.Type != record.AfterType {
			return RollbackResult{}, fmt.Errorf("%w: %s", ErrRollbackConflict, record.Path)
		}
	}
	if err := m.journal.restore(m.resolver.Root(), directory, manifest); err != nil {
		return RollbackResult{}, err
	}
	manifest.RolledBack = true
	if err := m.journal.writeManifest(directory, manifest); err != nil {
		return RollbackResult{}, err
	}
	result := RollbackResult{TransactionID: manifest.ID, RolledBack: true}
	for _, record := range manifest.Records {
		result.Files = append(result.Files, FileChange{Path: record.Path, Action: "restored", BeforeSHA256: record.AfterSHA256, AfterSHA256: record.BeforeSHA256})
	}
	return result, nil
}

func (m *Manager) normalizeOperations(ctx context.Context, operations []Operation) ([]Operation, []string, []string, error) {
	if len(operations) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: operations are required", ErrInvalidOperation)
	}
	if len(operations) > m.cfg.MaxTransactionFiles {
		return nil, nil, nil, ErrTooLarge
	}
	normalized := make([]Operation, len(operations))
	affected := make([]string, 0)
	stagePaths := make([]string, 0)
	produced := make(map[string]struct{})
	for index, operation := range operations {
		switch operation.Kind {
		case "replace", "create", "mkdir", "delete", "chmod":
			resolved, err := m.resolver.Resolve(operation.Path, operation.Kind == "create" || operation.Kind == "mkdir")
			if err != nil {
				return nil, nil, nil, err
			}
			operation.Path = resolved.Relative
			affected = append(affected, operation.Path)
			stagePaths = append(stagePaths, operation.Path)
			if operation.Kind == "create" || operation.Kind == "mkdir" {
				produced[operation.Path] = struct{}{}
			}
		case "copy":
			_, sourceProduced := produced[filepath.ToSlash(filepath.Clean(filepath.FromSlash(operation.From)))]
			from, err := m.resolver.Resolve(operation.From, sourceProduced)
			if err != nil {
				return nil, nil, nil, err
			}
			to, err := m.resolver.Resolve(operation.To, true)
			if err != nil {
				return nil, nil, nil, err
			}
			operation.From, operation.To = from.Relative, to.Relative
			affected = append(affected, operation.To)
			stagePaths = append(stagePaths, operation.From, operation.To)
			produced[operation.To] = struct{}{}
		case "move":
			_, sourceProduced := produced[filepath.ToSlash(filepath.Clean(filepath.FromSlash(operation.From)))]
			from, err := m.resolver.Resolve(operation.From, sourceProduced)
			if err != nil {
				return nil, nil, nil, err
			}
			to, err := m.resolver.Resolve(operation.To, true)
			if err != nil {
				return nil, nil, nil, err
			}
			operation.From, operation.To = from.Relative, to.Relative
			affected = append(affected, operation.From, operation.To)
			stagePaths = append(stagePaths, operation.From, operation.To)
			delete(produced, operation.From)
			produced[operation.To] = struct{}{}
		default:
			return nil, nil, nil, fmt.Errorf("%w: unsupported kind %q", ErrInvalidOperation, operation.Kind)
		}
		if err := m.authorize(ctx, operation); err != nil {
			return nil, nil, nil, err
		}
		normalized[index] = operation
	}
	return normalized, uniqueSorted(affected), uniqueSorted(stagePaths), nil
}

func prepareStage(workspace, stageRoot string, paths []string, cfg Config) error {
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return err
	}
	budget := &copyBudget{maxBytes: cfg.MaxTransactionBytes, maxFiles: cfg.MaxTransactionFiles}
	for _, relative := range reducePaths(paths) {
		source := filepath.Join(workspace, filepath.FromSlash(relative))
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyPath(source, filepath.Join(stageRoot, filepath.FromSlash(relative)), budget); err != nil {
			return err
		}
	}
	return nil
}

func buildTransactionResult(manifest *journalManifest, directory, root string, applied, rollbackAvailable bool, maxDiff int) (TransactionResult, error) {
	result := TransactionResult{TransactionID: manifest.ID, Applied: applied, RollbackAvailable: rollbackAvailable}
	var diff strings.Builder
	for _, record := range manifest.Records {
		action := changeAction(record)
		result.Files = append(result.Files, FileChange{Path: record.Path, Action: action, BeforeSHA256: record.BeforeSHA256, AfterSHA256: record.AfterSHA256})
		if diff.Len() >= maxDiff {
			result.DiffTruncated = true
			continue
		}
		piece, err := recordDiff(record, directory, root)
		if err != nil {
			return TransactionResult{}, err
		}
		remaining := maxDiff - diff.Len()
		if len(piece) > remaining {
			diff.WriteString(piece[:remaining])
			result.DiffTruncated = true
		} else {
			diff.WriteString(piece)
		}
	}
	result.Diff = diff.String()
	if !applied {
		result.TransactionID = ""
		result.RollbackAvailable = false
	}
	return result, nil
}

func changeAction(record journalRecord) string {
	switch {
	case !record.BeforeExists && record.AfterExists:
		return "created"
	case record.BeforeExists && !record.AfterExists:
		return "deleted"
	case record.BeforeType != record.AfterType:
		return "replaced"
	case record.BeforeSHA256 != record.AfterSHA256:
		return "modified"
	default:
		return "unchanged"
	}
}

func recordDiff(record journalRecord, directory, root string) (string, error) {
	if record.BeforeType == "directory" || record.AfterType == "directory" {
		return fmt.Sprintf("%s %s\n", strings.ToUpper(changeAction(record)), record.Path), nil
	}
	var before, after []byte
	var err error
	if record.BeforeExists {
		before, err = os.ReadFile(filepath.Join(directory, filepath.FromSlash(record.Snapshot)))
		if err != nil {
			return "", err
		}
	}
	if record.AfterExists {
		after, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(record.Path)))
		if err != nil {
			return "", err
		}
	}
	if !utf8.Valid(before) || !utf8.Valid(after) || bytes.IndexByte(before, 0) >= 0 || bytes.IndexByte(after, 0) >= 0 {
		return fmt.Sprintf("BINARY %s %s\n", strings.ToUpper(changeAction(record)), record.Path), nil
	}
	return fullFileDiff(record.Path, string(before), string(after)), nil
}

func fullFileDiff(path, before, after string) string {
	beforeLines := splitLines(before)
	afterLines := splitLines(after)
	var builder strings.Builder
	fmt.Fprintf(&builder, "--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", path, path, len(beforeLines), len(afterLines))
	for _, line := range beforeLines {
		builder.WriteByte('-')
		builder.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			builder.WriteByte('\n')
		}
	}
	for _, line := range afterLines {
		builder.WriteByte('+')
		builder.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}
