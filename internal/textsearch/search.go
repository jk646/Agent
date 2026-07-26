package textsearch

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/example/agent-shell-tool/internal/textsearchpolicy"
)

type scanResult struct {
	matches                                   []Match
	counts                                    map[string]int
	scannedFiles, skippedBinary, skippedLarge int
	truncated                                 bool
}

type searchOptions struct {
	params         SearchParams
	patterns       []compiledPattern
	include        []*regexp.Regexp
	exclude        []*regexp.Regexp
	extensions     map[string]struct{}
	maxDepth       int
	maxFileBytes   int64
	maxMatchesFile int
}

func (m *Manager) Search(ctx context.Context, params SearchParams) (SearchResult, error) {
	pattern := Pattern{Query: params.Query, Regex: params.Regex && !params.FixedString, CaseSensitive: params.CaseSensitive, WholeWord: params.WholeWord}
	return m.search(ctx, params, []Pattern{pattern})
}

func (m *Manager) Multi(ctx context.Context, params MultiParams) (SearchResult, error) {
	if len(params.Patterns) == 0 {
		return SearchResult{}, fmt.Errorf("%w: patterns are required", ErrInvalidRequest)
	}
	if params.InvertMatch {
		return SearchResult{}, fmt.Errorf("%w: invert_match is not supported with multiple patterns", ErrInvalidRequest)
	}
	return m.search(ctx, params.SearchParams, params.Patterns)
}

func (m *Manager) search(parent context.Context, params SearchParams, patterns []Pattern) (SearchResult, error) {
	started := time.Now()
	searchID, ctx, release, err := m.begin(parent, params.SearchID)
	if err != nil {
		return SearchResult{}, err
	}
	defer release()
	result, err := m.scan(ctx, params, patterns, true)
	if err != nil {
		return SearchResult{}, err
	}
	page, next, pageTruncated, err := paginate(result.matches, params.Cursor, params.Limit, m.cfg.DefaultLimit, m.cfg.MaxResults)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{SearchID: searchID, Matches: page, NextCursor: next, Truncated: result.truncated || pageTruncated, ScannedFiles: result.scannedFiles, SkippedBinary: result.skippedBinary, SkippedLarge: result.skippedLarge, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (m *Manager) Files(parent context.Context, params SearchParams) (FilesResult, error) {
	started := time.Now()
	searchID, ctx, release, err := m.begin(parent, params.SearchID)
	if err != nil {
		return FilesResult{}, err
	}
	defer release()
	pattern := Pattern{Query: params.Query, Regex: params.Regex && !params.FixedString, CaseSensitive: params.CaseSensitive, WholeWord: params.WholeWord}
	result, err := m.scan(ctx, params, []Pattern{pattern}, false)
	if err != nil {
		return FilesResult{}, err
	}
	paths := make([]string, 0, len(result.counts))
	for path := range result.counts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	page, next, pageTruncated, err := paginate(paths, params.Cursor, params.Limit, m.cfg.DefaultLimit, m.cfg.MaxResults)
	if err != nil {
		return FilesResult{}, err
	}
	return FilesResult{SearchID: searchID, Paths: page, NextCursor: next, Truncated: result.truncated || pageTruncated, ScannedFiles: result.scannedFiles, SkippedBinary: result.skippedBinary, SkippedLarge: result.skippedLarge, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (m *Manager) Count(parent context.Context, params SearchParams) (CountResult, error) {
	started := time.Now()
	searchID, ctx, release, err := m.begin(parent, params.SearchID)
	if err != nil {
		return CountResult{}, err
	}
	defer release()
	pattern := Pattern{Query: params.Query, Regex: params.Regex && !params.FixedString, CaseSensitive: params.CaseSensitive, WholeWord: params.WholeWord}
	result, err := m.scan(ctx, params, []Pattern{pattern}, false)
	if err != nil {
		return CountResult{}, err
	}
	files := make([]FileCount, 0, len(result.counts))
	total := 0
	for path, count := range result.counts {
		files = append(files, FileCount{Path: path, Count: count})
		total += count
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	page, next, pageTruncated, err := paginate(files, params.Cursor, params.Limit, m.cfg.DefaultLimit, m.cfg.MaxResults)
	if err != nil {
		return CountResult{}, err
	}
	return CountResult{SearchID: searchID, Files: page, Total: total, NextCursor: next, Truncated: result.truncated || pageTruncated, ScannedFiles: result.scannedFiles, SkippedBinary: result.skippedBinary, SkippedLarge: result.skippedLarge, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (m *Manager) scan(ctx context.Context, params SearchParams, patterns []Pattern, collectMatches bool) (scanResult, error) {
	resolved, err := m.resolver.Resolve(params.Path)
	if err != nil {
		return scanResult{}, err
	}
	if err := m.policy.Authorize(ctx, textsearchpolicy.Operation{Kind: "search_text", Path: resolved.Relative}); err != nil {
		return scanResult{}, err
	}
	options, err := m.makeOptions(params, patterns)
	if err != nil {
		return scanResult{}, err
	}
	result := scanResult{counts: make(map[string]int)}
	rootInfo, err := os.Lstat(resolved.Absolute)
	if err != nil {
		return scanResult{}, err
	}
	visit := func(current string, entry os.DirEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relativeRoot, err := filepath.Rel(resolved.Absolute, current)
		if err != nil {
			return err
		}
		relativeRoot = filepath.ToSlash(relativeRoot)
		if relativeRoot == "." {
			relativeRoot = entry.Name()
		}
		workspaceRelative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		workspaceRelative = filepath.ToSlash(workspaceRelative)
		if !options.acceptFile(relativeRoot, entry.Name()) {
			return nil
		}
		if result.scannedFiles >= m.cfg.MaxScannedFiles {
			result.truncated = true
			return filepath.SkipAll
		}
		result.scannedFiles++
		if info.Size() > options.maxFileBytes {
			result.skippedLarge++
			return nil
		}
		matches, count, fileTruncated, unsupported, err := searchFile(ctx, current, workspaceRelative, info.Size(), options)
		if err != nil {
			return err
		}
		if unsupported {
			result.skippedBinary++
			return nil
		}
		if count > 0 {
			result.counts[workspaceRelative] = count
		}
		if collectMatches {
			result.matches = append(result.matches, matches...)
			result.truncated = result.truncated || fileTruncated
			if len(result.matches) > m.cfg.MaxResults {
				result.matches = result.matches[:m.cfg.MaxResults]
				result.truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	}
	if rootInfo.Mode().IsRegular() {
		entry := fileInfoDirEntry{rootInfo}
		if err := visit(resolved.Absolute, entry); err != nil && err != filepath.SkipAll {
			return scanResult{}, err
		}
	} else if rootInfo.IsDir() {
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
				if depth > options.maxDepth {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if options.skipEntry(entry, m.cfg.IgnoredNames) {
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			return visit(current, entry)
		})
		if err != nil {
			return scanResult{}, err
		}
	}
	sort.SliceStable(result.matches, func(i, j int) bool {
		if result.matches[i].Path != result.matches[j].Path {
			return result.matches[i].Path < result.matches[j].Path
		}
		if result.matches[i].Line != result.matches[j].Line {
			return result.matches[i].Line < result.matches[j].Line
		}
		if result.matches[i].Column != result.matches[j].Column {
			return result.matches[i].Column < result.matches[j].Column
		}
		return result.matches[i].PatternID < result.matches[j].PatternID
	})
	return result, nil
}

func (m *Manager) makeOptions(params SearchParams, patterns []Pattern) (searchOptions, error) {
	if params.Cursor < 0 || params.ContextBefore < 0 || params.ContextAfter < 0 || params.ContextBefore > m.cfg.MaxContextLines || params.ContextAfter > m.cfg.MaxContextLines {
		return searchOptions{}, fmt.Errorf("%w: invalid cursor or context", ErrInvalidRequest)
	}
	options := searchOptions{params: params, extensions: make(map[string]struct{}), maxDepth: params.MaxDepth, maxFileBytes: params.MaxFileBytes, maxMatchesFile: params.MaxMatchesPerFile}
	if options.maxDepth <= 0 || options.maxDepth > m.cfg.MaxDepth {
		options.maxDepth = m.cfg.MaxDepth
	}
	if options.maxFileBytes <= 0 || options.maxFileBytes > m.cfg.MaxFileBytes {
		options.maxFileBytes = m.cfg.MaxFileBytes
	}
	if options.maxMatchesFile <= 0 || options.maxMatchesFile > m.cfg.MaxMatchesFile {
		options.maxMatchesFile = m.cfg.MaxMatchesFile
	}
	for _, pattern := range patterns {
		compiled, err := compilePattern(pattern)
		if err != nil {
			return searchOptions{}, err
		}
		options.patterns = append(options.patterns, compiled)
	}
	includes := append([]string{}, params.IncludePatterns...)
	if params.FilePattern != "" {
		includes = append(includes, params.FilePattern)
	}
	for _, pattern := range includes {
		compiled, err := compileGlob(pattern)
		if err != nil {
			return searchOptions{}, err
		}
		options.include = append(options.include, compiled)
	}
	for _, pattern := range params.ExcludePatterns {
		compiled, err := compileGlob(pattern)
		if err != nil {
			return searchOptions{}, err
		}
		options.exclude = append(options.exclude, compiled)
	}
	for _, extension := range params.Extensions {
		if extension = normalizeExtension(extension); extension != "" {
			options.extensions[extension] = struct{}{}
		}
	}
	return options, nil
}

func searchFile(ctx context.Context, path, relative string, size int64, options searchOptions) ([]Match, int, bool, bool, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, 0, false, false, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, size+1))
	if err != nil {
		return nil, 0, false, false, err
	}
	decoded, err := decodeText(data)
	if err == ErrUnsupportedEncoding {
		return nil, 0, false, true, nil
	}
	if err != nil {
		return nil, 0, false, false, err
	}
	lines, starts := splitLines(decoded.text)
	matches := make([]Match, 0)
	count := 0
	for lineIndex, line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, 0, false, false, err
		}
		for _, pattern := range options.patterns {
			locations := pattern.find(line)
			if options.params.InvertMatch {
				if len(locations) == 0 {
					locations = [][2]int{{0, 0}}
				} else {
					locations = nil
				}
			}
			for _, location := range locations {
				count++
				if len(matches) < options.maxMatchesFile {
					start := lineIndex - options.params.ContextBefore
					if start < 0 {
						start = 0
					}
					end := lineIndex + options.params.ContextAfter + 1
					if end > len(lines) {
						end = len(lines)
					}
					matched := line[location[0]:location[1]]
					matches = append(matches, Match{PatternID: pattern.id, Path: relative, Line: lineIndex + 1, Column: utf8.RuneCountInString(line[:location[0]]) + 1, ByteOffset: decoded.originalOffset(starts[lineIndex] + location[0]), Text: line, Match: matched, Before: append([]string(nil), lines[start:lineIndex]...), After: append([]string(nil), lines[lineIndex+1:end]...)})
				}
				if options.params.InvertMatch {
					break
				}
			}
		}
	}
	return matches, count, count > len(matches), false, nil
}

func splitLines(content string) ([]string, []int) {
	lines := make([]string, 0)
	starts := make([]int, 0)
	start := 0
	for index := 0; index < len(content); index++ {
		if content[index] != '\n' {
			continue
		}
		end := index
		if end > start && content[end-1] == '\r' {
			end--
		}
		lines = append(lines, content[start:end])
		starts = append(starts, start)
		start = index + 1
	}
	if start < len(content) || len(content) == 0 {
		lines = append(lines, strings.TrimSuffix(content[start:], "\r"))
		starts = append(starts, start)
	}
	return lines, starts
}

func (o searchOptions) acceptFile(relative, name string) bool {
	if len(o.extensions) > 0 {
		if _, ok := o.extensions[strings.ToLower(filepath.Ext(name))]; !ok {
			return false
		}
	}
	for _, pattern := range o.exclude {
		if pattern.MatchString(relative) || pattern.MatchString(name) {
			return false
		}
	}
	if len(o.include) == 0 {
		return true
	}
	for _, pattern := range o.include {
		if pattern.MatchString(relative) || pattern.MatchString(name) {
			return true
		}
	}
	return false
}

func (o searchOptions) skipEntry(entry os.DirEntry, ignored []string) bool {
	if entry.Type()&os.ModeSymlink != 0 {
		return true
	}
	if !o.params.IncludeHidden && strings.HasPrefix(entry.Name(), ".") {
		return true
	}
	for _, name := range ignored {
		if entry.Name() == name {
			return true
		}
	}
	return false
}

func compileGlob(pattern string) (*regexp.Regexp, error) {
	if pattern == "" || strings.IndexByte(pattern, 0) >= 0 {
		return nil, fmt.Errorf("%w: invalid file pattern", ErrInvalidRequest)
	}
	var builder strings.Builder
	builder.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch pattern[index] {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				if index+2 < len(pattern) && pattern[index+2] == '/' {
					builder.WriteString("(?:.*/)?")
					index += 2
				} else {
					builder.WriteString(".*")
					index++
				}
			} else {
				builder.WriteString("[^/]*")
			}
		case '?':
			builder.WriteString("[^/]")
		case '[', ']', '{', '}', '(', ')', '+', '.', '^', '$', '|', '\\':
			builder.WriteByte('\\')
			builder.WriteByte(pattern[index])
		default:
			builder.WriteByte(pattern[index])
		}
	}
	builder.WriteString("$")
	compiled, err := regexp.Compile(builder.String())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid file pattern", ErrInvalidRequest)
	}
	return compiled, nil
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

func paginate[T any](items []T, cursor, limit, fallback, maximum int) ([]T, int, bool, error) {
	if cursor < 0 || cursor > len(items) {
		return nil, 0, false, fmt.Errorf("%w: invalid cursor", ErrInvalidRequest)
	}
	if limit <= 0 {
		limit = fallback
	}
	if limit > maximum {
		limit = maximum
	}
	end := cursor + limit
	if end > len(items) {
		end = len(items)
	}
	next := 0
	if end < len(items) {
		next = end
	}
	return items[cursor:end], next, end < len(items), nil
}

type fileInfoDirEntry struct{ os.FileInfo }

func (entry fileInfoDirEntry) Type() os.FileMode          { return entry.Mode().Type() }
func (entry fileInfoDirEntry) Info() (os.FileInfo, error) { return entry.FileInfo, nil }
