package filesearch

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/agent-shell-tool/internal/searchpolicy"
)

func (m *Manager) Find(parent context.Context, params FindParams) (FindResult, error) {
	started := time.Now()
	searchID, ctx, release, err := m.begin(parent, params.SearchID)
	if err != nil {
		return FindResult{}, err
	}
	defer release()
	resolved, err := m.resolver.Resolve(params.Path)
	if err != nil {
		return FindResult{}, err
	}
	if err := m.policy.Authorize(ctx, searchpolicy.Operation{Kind: "find", Path: resolved.Relative}); err != nil {
		return FindResult{}, err
	}
	filter, err := m.newFindFilter(params)
	if err != nil {
		return FindResult{}, err
	}
	matches := make([]Entry, 0)
	scanned := 0
	truncated := false
	err = filepath.WalkDir(resolved.Absolute, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == resolved.Absolute {
			return nil
		}
		depth, err := relativeDepth(resolved.Absolute, current)
		if err != nil {
			return err
		}
		if depth > filter.maxDepth {
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
		scanned++
		if scanned > m.cfg.MaxScannedEntries {
			truncated = true
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		score, matched := filter.match(filepath.ToSlash(relative), info)
		if matched {
			matches = append(matches, makeEntry(relative, info, score))
			if len(matches) >= m.cfg.MaxResults {
				truncated = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return FindResult{}, err
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Path < matches[j].Path
	})
	page, next, pageTruncated, err := paginate(matches, params.Cursor, params.Limit, m.cfg.MaxResults)
	if err != nil {
		return FindResult{}, err
	}
	return FindResult{
		SearchID: searchID, Matches: page, NextCursor: next, Truncated: truncated || pageTruncated,
		ScannedEntries: scanned, DurationMS: time.Since(started).Milliseconds(),
	}, nil
}

type findFilter struct {
	pattern        string
	name           string
	typeName       string
	extensions     map[string]struct{}
	maxDepth       int
	minSize        int64
	maxSize        int64
	modifiedAfter  time.Time
	modifiedBefore time.Time
}

func (m *Manager) newFindFilter(params FindParams) (findFilter, error) {
	filter := findFilter{
		pattern: params.Pattern, name: strings.ToLower(params.Name), typeName: params.Type,
		maxDepth: params.MaxDepth, minSize: params.MinSize, maxSize: params.MaxSize,
		extensions: make(map[string]struct{}),
	}
	if filter.maxDepth <= 0 || filter.maxDepth > m.cfg.MaxDepth {
		filter.maxDepth = m.cfg.MaxDepth
	}
	if params.Cursor < 0 || params.MinSize < 0 || params.MaxSize < 0 || (params.MaxSize > 0 && params.MinSize > params.MaxSize) {
		return findFilter{}, fmt.Errorf("%w: invalid cursor or size range", ErrInvalidRequest)
	}
	if params.Pattern != "" {
		if _, err := path.Match(params.Pattern, "probe"); err != nil {
			return findFilter{}, fmt.Errorf("%w: invalid glob pattern", ErrInvalidRequest)
		}
	}
	switch params.Type {
	case "", "any", "file", "directory":
	default:
		return findFilter{}, fmt.Errorf("%w: invalid type", ErrInvalidRequest)
	}
	for _, extension := range params.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		filter.extensions[extension] = struct{}{}
	}
	var err error
	if params.ModifiedAfter != "" {
		filter.modifiedAfter, err = time.Parse(time.RFC3339, params.ModifiedAfter)
		if err != nil {
			return findFilter{}, fmt.Errorf("%w: invalid modified_after", ErrInvalidRequest)
		}
	}
	if params.ModifiedBefore != "" {
		filter.modifiedBefore, err = time.Parse(time.RFC3339, params.ModifiedBefore)
		if err != nil {
			return findFilter{}, fmt.Errorf("%w: invalid modified_before", ErrInvalidRequest)
		}
	}
	return filter, nil
}

func (f findFilter) match(relative string, info os.FileInfo) (int, bool) {
	if f.typeName == "file" && !info.Mode().IsRegular() || f.typeName == "directory" && !info.IsDir() {
		return 0, false
	}
	if info.Size() < f.minSize || f.maxSize > 0 && info.Size() > f.maxSize {
		return 0, false
	}
	if !f.modifiedAfter.IsZero() && !info.ModTime().After(f.modifiedAfter) {
		return 0, false
	}
	if !f.modifiedBefore.IsZero() && !info.ModTime().Before(f.modifiedBefore) {
		return 0, false
	}
	if len(f.extensions) > 0 {
		if _, exists := f.extensions[strings.ToLower(filepath.Ext(info.Name()))]; !exists {
			return 0, false
		}
	}
	base := info.Name()
	lowerBase := strings.ToLower(base)
	score := 1
	if f.name != "" {
		switch {
		case lowerBase == f.name:
			score = 100
		case strings.HasPrefix(lowerBase, f.name):
			score = 80
		case strings.Contains(lowerBase, f.name):
			score = 60
		default:
			return 0, false
		}
	}
	if f.pattern != "" {
		baseMatched, _ := path.Match(f.pattern, base)
		pathMatched, _ := path.Match(f.pattern, relative)
		if !baseMatched && !pathMatched {
			return 0, false
		}
		if score < 40 {
			score = 40
		}
	}
	return score, true
}

func paginate[T any](items []T, cursor, limit, maximum int) ([]T, int, bool, error) {
	if cursor < 0 || cursor > len(items) {
		return nil, 0, false, fmt.Errorf("%w: invalid cursor", ErrInvalidRequest)
	}
	if limit <= 0 || limit > maximum {
		limit = maximum
	}
	end := cursor + limit
	if end > len(items) {
		end = len(items)
	}
	truncated := end < len(items)
	next := 0
	if truncated {
		next = end
	}
	return items[cursor:end], next, truncated, nil
}
