package filetool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/example/agent-shell-tool/internal/filepolicy"
)

func (m *Manager) Stat(ctx context.Context, params StatParams) (FileInfo, error) {
	resolved, err := m.resolver.Resolve(params.Path, false)
	if err != nil {
		return FileInfo{}, err
	}
	if err := m.policy.Authorize(ctx, fileOperation("stat", resolved.Relative)); err != nil {
		return FileInfo{}, err
	}
	return m.fileInfo(resolved.Relative, resolved.Absolute, params.IncludeHash)
}

func (m *Manager) Read(ctx context.Context, params ReadParams) (ReadResult, error) {
	resolved, err := m.resolver.Resolve(params.Path, false)
	if err != nil {
		return ReadResult{}, err
	}
	if err := m.policy.Authorize(ctx, fileOperation("read", resolved.Relative)); err != nil {
		return ReadResult{}, err
	}
	info, err := os.Stat(resolved.Absolute)
	if err != nil {
		return ReadResult{}, mapPathError(err)
	}
	if !info.Mode().IsRegular() {
		return ReadResult{}, fmt.Errorf("%w: read requires a regular file", ErrInvalidOperation)
	}
	if info.Size() > m.cfg.MaxFileBytes {
		return ReadResult{}, ErrTooLarge
	}
	content, err := os.ReadFile(resolved.Absolute)
	if err != nil {
		return ReadResult{}, err
	}
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return ReadResult{}, ErrBinaryFile
	}
	hash := sha256.Sum256(content)
	lines := splitLines(string(content))
	start := params.StartLine
	if start <= 0 {
		start = 1
	}
	if start > len(lines)+1 {
		return ReadResult{}, fmt.Errorf("%w: start_line exceeds file length", ErrInvalidOperation)
	}
	end := params.EndLine
	if end <= 0 || end > len(lines) {
		end = len(lines)
	}
	if end < start-1 {
		return ReadResult{}, fmt.Errorf("%w: end_line precedes start_line", ErrInvalidOperation)
	}
	maxBytes := params.MaxBytes
	if maxBytes <= 0 || maxBytes > m.cfg.MaxReadBytes {
		maxBytes = m.cfg.MaxReadBytes
	}
	selected, actualEnd, truncated := selectLines(lines, start, end, maxBytes)
	result := ReadResult{
		Path:       resolved.Relative,
		Content:    selected,
		SHA256:     hex.EncodeToString(hash[:]),
		StartLine:  start,
		EndLine:    actualEnd,
		TotalLines: len(lines),
		Truncated:  truncated,
		Newline:    detectNewline(content),
	}
	if truncated && actualEnd < len(lines) {
		result.NextLine = actualEnd + 1
	}
	return result, nil
}

func (m *Manager) List(ctx context.Context, params ListParams) (ListResult, error) {
	resolved, err := m.resolver.Resolve(params.Path, false)
	if err != nil {
		return ListResult{}, err
	}
	if err := m.policy.Authorize(ctx, fileOperation("list", resolved.Relative)); err != nil {
		return ListResult{}, err
	}
	depth := params.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > 20 {
		return ListResult{}, ErrTooLarge
	}
	limit := params.Limit
	if limit <= 0 || limit > m.cfg.MaxEntries {
		limit = m.cfg.MaxEntries
	}
	entries, err := m.walkEntries(resolved, depth, params.IncludeHidden, params.IncludeHash)
	if err != nil {
		return ListResult{}, err
	}
	start := params.Cursor
	if start < 0 || start > len(entries) {
		return ListResult{}, fmt.Errorf("%w: invalid cursor", ErrInvalidOperation)
	}
	end := start + limit
	if end > len(entries) {
		end = len(entries)
	}
	result := ListResult{Path: resolved.Relative, Entries: entries[start:end], Truncated: end < len(entries)}
	if result.Truncated {
		result.NextCursor = end
	}
	return result, nil
}

func (m *Manager) Find(ctx context.Context, params FindParams) (ListResult, error) {
	if params.Pattern == "" {
		return ListResult{}, fmt.Errorf("%w: pattern is required", ErrInvalidOperation)
	}
	resolved, err := m.resolver.Resolve(params.Path, false)
	if err != nil {
		return ListResult{}, err
	}
	if err := m.policy.Authorize(ctx, fileOperation("find", resolved.Relative)); err != nil {
		return ListResult{}, err
	}
	limit := params.Limit
	if limit <= 0 || limit > m.cfg.MaxEntries {
		limit = m.cfg.MaxEntries
	}
	entries := make([]FileInfo, 0)
	truncated := false
	err = filepath.WalkDir(resolved.Absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == resolved.Absolute {
			return nil
		}
		if !params.IncludeHidden && isHidden(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relativeBase, err := filepath.Rel(resolved.Absolute, current)
		if err != nil {
			return err
		}
		matched, err := path.Match(params.Pattern, filepath.ToSlash(relativeBase))
		if err != nil {
			return fmt.Errorf("%w: invalid glob pattern", ErrInvalidOperation)
		}
		if !matched {
			matched, _ = path.Match(params.Pattern, entry.Name())
		}
		if !matched || !matchesType(entry, params.Type) {
			return nil
		}
		if len(entries) >= limit {
			truncated = true
			return filepath.SkipAll
		}
		workspaceRelative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		info, err := m.fileInfo(filepath.ToSlash(workspaceRelative), current, false)
		if err != nil {
			return err
		}
		entries = append(entries, info)
		return nil
	})
	if err != nil {
		return ListResult{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return ListResult{Path: resolved.Relative, Entries: entries, Truncated: truncated}, nil
}

func (m *Manager) Search(ctx context.Context, params SearchParams) (SearchResult, error) {
	if params.Query == "" {
		return SearchResult{}, fmt.Errorf("%w: query is required", ErrInvalidOperation)
	}
	resolved, err := m.resolver.Resolve(params.Path, false)
	if err != nil {
		return SearchResult{}, err
	}
	if err := m.policy.Authorize(ctx, fileOperation("search", resolved.Relative)); err != nil {
		return SearchResult{}, err
	}
	limit := params.Limit
	if limit <= 0 || limit > m.cfg.MaxSearchMatches {
		limit = m.cfg.MaxSearchMatches
	}
	matcher, err := newSearchMatcher(params)
	if err != nil {
		return SearchResult{}, err
	}
	result := SearchResult{Matches: make([]SearchMatch, 0)}
	err = filepath.WalkDir(resolved.Absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if current != resolved.Absolute && !params.IncludeHidden && isHidden(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || (!params.IncludeHidden && isHidden(entry.Name())) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > m.cfg.MaxFileBytes {
			return err
		}
		matches, binary, err := searchFile(current, matcher, limit-len(result.Matches))
		if err != nil {
			return err
		}
		if binary {
			return nil
		}
		workspaceRelative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		for index := range matches {
			matches[index].Path = filepath.ToSlash(workspaceRelative)
		}
		result.Matches = append(result.Matches, matches...)
		if len(result.Matches) >= limit {
			result.Truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return result, err
}

func (m *Manager) walkEntries(resolved ResolvedPath, depth int, includeHidden, includeHash bool) ([]FileInfo, error) {
	rootInfo, err := os.Stat(resolved.Absolute)
	if err != nil {
		return nil, mapPathError(err)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("%w: list requires a directory", ErrInvalidOperation)
	}
	entries := make([]FileInfo, 0)
	err = filepath.WalkDir(resolved.Absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == resolved.Absolute {
			return nil
		}
		relativeBase, err := filepath.Rel(resolved.Absolute, current)
		if err != nil {
			return err
		}
		currentDepth := len(strings.Split(filepath.Clean(relativeBase), string(filepath.Separator)))
		if currentDepth > depth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !includeHidden && isHidden(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		workspaceRelative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		info, err := m.fileInfo(filepath.ToSlash(workspaceRelative), current, includeHash)
		if err != nil {
			return err
		}
		entries = append(entries, info)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, err
}

func (m *Manager) fileInfo(relative, absolute string, includeHash bool) (FileInfo, error) {
	info, err := os.Lstat(absolute)
	if err != nil {
		return FileInfo{}, mapPathError(err)
	}
	result := FileInfo{Path: relative, Type: fileType(info), Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()), ModifiedAt: info.ModTime().UTC().Format(timeFormat)}
	if includeHash && info.Mode()&os.ModeSymlink == 0 {
		if info.Mode().IsRegular() && info.Size() > m.cfg.MaxFileBytes {
			return result, nil
		}
		result.SHA256, err = digestPath(absolute)
	}
	return result, err
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

func fileType(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	case info.IsDir():
		return "directory"
	case info.Mode().IsRegular():
		return "file"
	default:
		return "other"
	}
}

func hashFile(path string) (string, error) {
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, handle); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func selectLines(lines []string, start, end, maxBytes int) (string, int, bool) {
	if start > len(lines) || end < start {
		return "", start - 1, false
	}
	var builder strings.Builder
	actualEnd := start - 1
	for index := start - 1; index < end; index++ {
		line := lines[index]
		if builder.Len()+len(line) > maxBytes {
			if builder.Len() == 0 {
				remaining := maxBytes
				for remaining > 0 && !utf8.ValidString(line[:remaining]) {
					remaining--
				}
				builder.WriteString(line[:remaining])
			}
			return builder.String(), actualEnd, true
		}
		builder.WriteString(line)
		actualEnd = index + 1
	}
	return builder.String(), actualEnd, false
}

func detectNewline(content []byte) string {
	if len(content) == 0 {
		return "none"
	}
	crlf := bytes.Count(content, []byte("\r\n"))
	lf := bytes.Count(content, []byte("\n"))
	switch {
	case lf == 0:
		return "none"
	case crlf == lf:
		return "crlf"
	case crlf == 0:
		return "lf"
	default:
		return "mixed"
	}
}

func isHidden(name string) bool { return strings.HasPrefix(name, ".") }

func matchesType(entry os.DirEntry, expected string) bool {
	switch expected {
	case "", "any":
		return true
	case "file":
		return entry.Type().IsRegular()
	case "directory":
		return entry.IsDir()
	case "symlink":
		return entry.Type()&os.ModeSymlink != 0
	default:
		return false
	}
}

type searchMatcher func(string) (int, bool)

func newSearchMatcher(params SearchParams) (searchMatcher, error) {
	query := params.Query
	if !params.CaseSensitive {
		query = strings.ToLower(query)
	}
	if !params.Regex {
		return func(line string) (int, bool) {
			candidate := line
			if !params.CaseSensitive {
				candidate = strings.ToLower(candidate)
			}
			index := strings.Index(candidate, query)
			return index, index >= 0
		}, nil
	}
	pattern := params.Query
	if !params.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid regular expression", ErrInvalidOperation)
	}
	return func(line string) (int, bool) {
		location := compiled.FindStringIndex(line)
		if location == nil {
			return -1, false
		}
		return location[0], true
	}, nil
}

func searchFile(path string, matcher searchMatcher, limit int) ([]SearchMatch, bool, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer handle.Close()
	reader := bufio.NewReader(handle)
	matches := make([]SearchMatch, 0)
	lineNumber := 0
	for {
		line, readErr := reader.ReadString('\n')
		if strings.IndexByte(line, 0) >= 0 || !utf8.ValidString(line) {
			return nil, true, nil
		}
		if line != "" {
			lineNumber++
			if index, matched := matcher(line); matched {
				matches = append(matches, SearchMatch{Line: lineNumber, Column: utf8.RuneCountInString(line[:index]) + 1, Text: strings.TrimRight(line, "\r\n")})
				if len(matches) >= limit {
					return matches, false, nil
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return matches, false, nil
			}
			return nil, false, readErr
		}
	}
}

func fileOperation(kind, path string) filepolicy.Operation {
	return filepolicy.Operation{Kind: kind, Path: path}
}

func mapPathError(err error) error {
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}
