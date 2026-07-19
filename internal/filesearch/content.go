package filesearch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/agent-shell-tool/internal/searchpolicy"
)

type lineMatcher func(string) (int, bool)

func (m *Manager) Content(parent context.Context, params ContentParams) (ContentResult, error) {
	started := time.Now()
	searchID, ctx, release, err := m.begin(parent, params.SearchID)
	if err != nil {
		return ContentResult{}, err
	}
	defer release()
	if params.Query == "" {
		return ContentResult{}, fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	if params.Cursor < 0 || params.ContextBefore < 0 || params.ContextAfter < 0 || params.ContextBefore > 20 || params.ContextAfter > 20 {
		return ContentResult{}, fmt.Errorf("%w: invalid cursor or context range", ErrInvalidRequest)
	}
	if params.FilePattern != "" {
		if _, err := path.Match(params.FilePattern, "probe"); err != nil {
			return ContentResult{}, fmt.Errorf("%w: invalid file_pattern", ErrInvalidRequest)
		}
	}
	resolved, err := m.resolver.Resolve(params.Path)
	if err != nil {
		return ContentResult{}, err
	}
	if err := m.policy.Authorize(ctx, searchpolicy.Operation{Kind: "content", Path: resolved.Relative}); err != nil {
		return ContentResult{}, err
	}
	matcher, err := newLineMatcher(params)
	if err != nil {
		return ContentResult{}, err
	}
	maxDepth := params.MaxDepth
	if maxDepth <= 0 || maxDepth > m.cfg.MaxDepth {
		maxDepth = m.cfg.MaxDepth
	}
	matches := make([]ContentMatch, 0)
	scannedEntries, scannedFiles, skippedBinary, skippedLarge := 0, 0, 0, 0
	truncated := false
	err = filepath.WalkDir(resolved.Absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current != resolved.Absolute {
			depth, err := relativeDepth(resolved.Absolute, current)
			if err != nil {
				return err
			}
			if depth > maxDepth {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if m.skipEntry(entry, params.IncludeHidden) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		scannedEntries++
		if scannedEntries > m.cfg.MaxScannedEntries {
			truncated = true
			return filepath.SkipAll
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if params.FilePattern != "" {
			baseMatched, _ := path.Match(params.FilePattern, entry.Name())
			relative, _ := filepath.Rel(resolved.Absolute, current)
			pathMatched, _ := path.Match(params.FilePattern, filepath.ToSlash(relative))
			if !baseMatched && !pathMatched {
				return nil
			}
		}
		if info.Size() > m.cfg.MaxFileBytes {
			skippedLarge++
			return nil
		}
		scannedFiles++
		fileMatches, binary, err := searchContentFile(current, matcher, params.ContextBefore, params.ContextAfter)
		if err != nil {
			return err
		}
		if binary {
			skippedBinary++
			return nil
		}
		relative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		for index := range fileMatches {
			fileMatches[index].Path = filepath.ToSlash(relative)
		}
		matches = append(matches, fileMatches...)
		if len(matches) >= m.cfg.MaxResults {
			matches = matches[:m.cfg.MaxResults]
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return ContentResult{}, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	page, next, pageTruncated, err := paginate(matches, params.Cursor, params.Limit, m.cfg.MaxResults)
	if err != nil {
		return ContentResult{}, err
	}
	return ContentResult{
		SearchID: searchID, Matches: page, NextCursor: next, Truncated: truncated || pageTruncated,
		ScannedFiles: scannedFiles, SkippedBinary: skippedBinary, SkippedLarge: skippedLarge,
		DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

func newLineMatcher(params ContentParams) (lineMatcher, error) {
	if !params.Regex {
		if params.CaseSensitive {
			return func(line string) (int, bool) {
				index := strings.Index(line, params.Query)
				return index, index >= 0
			}, nil
		}
		compiled, err := regexp.Compile("(?i)" + regexp.QuoteMeta(params.Query))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid query", ErrInvalidRequest)
		}
		return func(line string) (int, bool) {
			location := compiled.FindStringIndex(line)
			if location == nil {
				return -1, false
			}
			return location[0], true
		}, nil
	}
	pattern := params.Query
	if !params.CaseSensitive {
		pattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid regular expression", ErrInvalidRequest)
	}
	return func(line string) (int, bool) {
		location := compiled.FindStringIndex(line)
		if location == nil {
			return -1, false
		}
		return location[0], true
	}, nil
}

func searchContentFile(filePath string, matcher lineMatcher, beforeCount, afterCount int) ([]ContentMatch, bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, err
	}
	if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
		return nil, true, nil
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	matches := make([]ContentMatch, 0)
	for index, line := range lines {
		column, matched := matcher(line)
		if !matched {
			continue
		}
		start := index - beforeCount
		if start < 0 {
			start = 0
		}
		end := index + afterCount + 1
		if end > len(lines) {
			end = len(lines)
		}
		match := ContentMatch{Line: index + 1, Column: utf8.RuneCountInString(line[:column]) + 1, Text: line}
		match.Before = append(match.Before, lines[start:index]...)
		match.After = append(match.After, lines[index+1:end]...)
		matches = append(matches, match)
	}
	return matches, false, nil
}

func (m *Manager) skipEntry(entry os.DirEntry, includeHidden bool) bool {
	if entry.Type()&os.ModeSymlink != 0 {
		return true
	}
	if !includeHidden && strings.HasPrefix(entry.Name(), ".") {
		return true
	}
	for _, ignored := range m.cfg.IgnoredNames {
		if entry.Name() == ignored {
			return true
		}
	}
	return false
}

func relativeDepth(root, current string) (int, error) {
	relative, err := filepath.Rel(root, current)
	if err != nil {
		return 0, err
	}
	if relative == "." {
		return 0, nil
	}
	return len(strings.Split(filepath.Clean(relative), string(filepath.Separator))), nil
}
