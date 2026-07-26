package folderreader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (m *Manager) Stat(ctx context.Context, params StatParams) (FolderStat, error) {
	resolved, info, err := m.resolveFolder(ctx, "stat", params.Path)
	if err != nil {
		return FolderStat{}, err
	}
	children, err := os.ReadDir(resolved.Absolute)
	if err != nil {
		return FolderStat{}, err
	}
	result := FolderStat{
		Path: resolved.Relative, Type: "folder", Mode: fmt.Sprintf("%04o", info.Mode().Perm()),
		ModifiedAt: info.ModTime().UTC().Format(timeFormat),
	}
	hash := sha256.New()
	for _, child := range children {
		if m.isIgnored(child.Name(), params.IncludeHidden) {
			continue
		}
		if child.Type()&os.ModeSymlink != 0 {
			result.OtherCount++
			continue
		}
		childInfo, err := child.Info()
		if err != nil {
			return FolderStat{}, err
		}
		switch {
		case childInfo.IsDir():
			result.FolderCount++
		case childInfo.Mode().IsRegular():
			result.FileCount++
		default:
			result.OtherCount++
		}
		if params.IncludeDigest {
			writeDigestEntry(hash, makeEntry(child.Name(), 1, childInfo))
		}
	}
	result.Empty = result.FileCount+result.FolderCount+result.OtherCount == 0
	if params.IncludeDigest {
		result.Digest = hex.EncodeToString(hash.Sum(nil))
	}
	return result, nil
}

func (m *Manager) List(parent context.Context, params ListParams) (ListResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return ListResult{}, err
	}
	defer release()
	if params.Cursor < 0 || params.Limit < 0 || params.MinSize < 0 || params.MaxSize < 0 || params.MaxSize > 0 && params.MinSize > params.MaxSize {
		return ListResult{}, fmt.Errorf("%w: invalid cursor or size range", ErrInvalidRequest)
	}
	resolved, _, err := m.resolveFolder(ctx, "list", params.Path)
	if err != nil {
		return ListResult{}, err
	}
	filter, err := newListFilter(params)
	if err != nil {
		return ListResult{}, err
	}
	walked, err := m.walk(ctx, resolved, walkOptions{depth: params.Depth, includeHidden: params.IncludeHidden, maxEntries: m.cfg.MaxResults})
	if err != nil {
		return ListResult{}, err
	}
	entries := make([]Entry, 0, len(walked.entries))
	for _, entry := range walked.entries {
		if filter.match(entry) {
			entries = append(entries, entry)
		}
	}
	if err := sortEntries(entries, params.SortBy, params.SortOrder); err != nil {
		return ListResult{}, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = m.cfg.DefaultLimit
	}
	if limit > m.cfg.MaxResults {
		limit = m.cfg.MaxResults
	}
	if params.Cursor > len(entries) {
		return ListResult{}, fmt.Errorf("%w: cursor exceeds result length", ErrInvalidRequest)
	}
	end := params.Cursor + limit
	if end > len(entries) {
		end = len(entries)
	}
	truncated := walked.truncated || end < len(entries)
	result := ListResult{
		ReadID: readID, Path: resolved.Relative, Entries: entries[params.Cursor:end], Truncated: truncated,
		ScannedEntries: walked.scannedEntries, SkippedSymlinks: walked.skippedSymlinks,
	}
	if end < len(entries) {
		result.NextCursor = end
	}
	return result, nil
}

type listFilter struct {
	includeFiles   bool
	includeFolders bool
	pattern        string
	extensions     map[string]struct{}
	minSize        int64
	maxSize        int64
	modifiedAfter  time.Time
	modifiedBefore time.Time
}

func newListFilter(params ListParams) (listFilter, error) {
	filter := listFilter{
		includeFiles: boolDefault(params.IncludeFiles, true), includeFolders: boolDefault(params.IncludeFolders, true),
		pattern: params.NamePattern, extensions: make(map[string]struct{}), minSize: params.MinSize, maxSize: params.MaxSize,
	}
	if params.NamePattern != "" {
		if _, err := path.Match(params.NamePattern, "probe"); err != nil {
			return listFilter{}, fmt.Errorf("%w: invalid name_pattern", ErrInvalidRequest)
		}
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
			return listFilter{}, fmt.Errorf("%w: invalid modified_after", ErrInvalidRequest)
		}
	}
	if params.ModifiedBefore != "" {
		filter.modifiedBefore, err = time.Parse(time.RFC3339, params.ModifiedBefore)
		if err != nil {
			return listFilter{}, fmt.Errorf("%w: invalid modified_before", ErrInvalidRequest)
		}
	}
	return filter, nil
}

func (f listFilter) match(entry Entry) bool {
	if entry.Type == "folder" && !f.includeFolders || entry.Type != "folder" && !f.includeFiles {
		return false
	}
	if f.pattern != "" {
		nameMatched, _ := path.Match(f.pattern, entry.Name)
		pathMatched, _ := path.Match(f.pattern, entry.Path)
		if !nameMatched && !pathMatched {
			return false
		}
	}
	if len(f.extensions) > 0 {
		if entry.Type != "file" {
			return false
		}
		if _, exists := f.extensions[entry.Extension]; !exists {
			return false
		}
	}
	if entry.Size < f.minSize || f.maxSize > 0 && entry.Size > f.maxSize {
		return false
	}
	modified, err := time.Parse(timeFormat, entry.ModifiedAt)
	if err != nil {
		return false
	}
	if !f.modifiedAfter.IsZero() && !modified.After(f.modifiedAfter) {
		return false
	}
	if !f.modifiedBefore.IsZero() && !modified.Before(f.modifiedBefore) {
		return false
	}
	return true
}

func sortEntries(entries []Entry, sortBy, order string) error {
	if sortBy == "" {
		sortBy = "path"
	}
	if order == "" {
		order = "asc"
	}
	if order != "asc" && order != "desc" {
		return fmt.Errorf("%w: invalid sort_order", ErrInvalidRequest)
	}
	valid := map[string]bool{"name": true, "path": true, "type": true, "size": true, "modified_at": true}
	if !valid[sortBy] {
		return fmt.Errorf("%w: invalid sort_by", ErrInvalidRequest)
	}
	less := func(left, right Entry) bool {
		var result bool
		switch sortBy {
		case "name":
			result = left.Name < right.Name
		case "type":
			result = left.Type < right.Type
		case "size":
			if left.Size == right.Size {
				result = left.Path < right.Path
			} else {
				result = left.Size < right.Size
			}
		case "modified_at":
			result = left.ModifiedAt < right.ModifiedAt
		default:
			result = left.Path < right.Path
		}
		if order == "desc" {
			return !result && !entriesEqualForSort(left, right, sortBy)
		}
		return result
	}
	sort.SliceStable(entries, func(i, j int) bool { return less(entries[i], entries[j]) })
	return nil
}

func entriesEqualForSort(left, right Entry, sortBy string) bool {
	switch sortBy {
	case "name":
		return left.Name == right.Name
	case "type":
		return left.Type == right.Type
	case "size":
		return left.Size == right.Size && left.Path == right.Path
	case "modified_at":
		return left.ModifiedAt == right.ModifiedAt
	default:
		return left.Path == right.Path
	}
}

func (m *Manager) Tree(parent context.Context, params TreeParams) (TreeResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return TreeResult{}, err
	}
	defer release()
	resolved, rootInfo, err := m.resolveFolder(ctx, "tree", params.Path)
	if err != nil {
		return TreeResult{}, err
	}
	maxEntries := params.MaxEntries
	if maxEntries <= 0 || maxEntries > m.cfg.MaxResults {
		maxEntries = m.cfg.MaxResults
	}
	walked, err := m.walk(ctx, resolved, walkOptions{depth: params.Depth, includeHidden: params.IncludeHidden, maxEntries: maxEntries})
	if err != nil {
		return TreeResult{}, err
	}
	rootName := filepath.Base(resolved.Absolute)
	if resolved.Relative == "" {
		rootName = "."
	}
	root := &TreeNode{
		Path: resolved.Relative, Name: rootName, Type: "folder", Mode: fmt.Sprintf("%04o", rootInfo.Mode().Perm()),
		ModifiedAt: rootInfo.ModTime().UTC().Format(timeFormat), Children: make([]*TreeNode, 0),
	}
	nodes := map[string]*TreeNode{resolved.Relative: root}
	includeFiles := boolDefault(params.IncludeFiles, true)
	entryCount := 0
	for _, entry := range walked.entries {
		if entry.Type != "folder" && !includeFiles {
			continue
		}
		node := &TreeNode{Path: entry.Path, Name: entry.Name, Type: entry.Type, Size: entry.Size, Mode: entry.Mode, ModifiedAt: entry.ModifiedAt}
		if entry.Type == "folder" {
			node.Children = make([]*TreeNode, 0)
			nodes[entry.Path] = node
		}
		parent := nodes[parentPath(entry.Path)]
		if parent != nil {
			parent.Children = append(parent.Children, node)
			entryCount++
		}
	}
	sortTree(root)
	return TreeResult{
		ReadID: readID, Root: root, EntryCount: entryCount, ScannedEntries: walked.scannedEntries,
		SkippedSymlinks: walked.skippedSymlinks, Truncated: walked.truncated,
	}, nil
}

func sortTree(node *TreeNode) {
	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].Type != node.Children[j].Type {
			return node.Children[i].Type == "folder"
		}
		return node.Children[i].Name < node.Children[j].Name
	})
	for _, child := range node.Children {
		if child.Type == "folder" {
			sortTree(child)
		}
	}
}

func writeDigestEntry(hash interface{ Write([]byte) (int, error) }, entry Entry) {
	fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\x00%s\x00%s\n", entry.Path, entry.Type, entry.Size, entry.Mode, entry.ModifiedAt, entry.SHA256)
}
