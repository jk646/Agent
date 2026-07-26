package folderreader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type walkOptions struct {
	depth         int
	includeHidden bool
	maxEntries    int
}

type walkResult struct {
	entries         []Entry
	scannedEntries  int
	skippedSymlinks int
	truncated       bool
}

func (m *Manager) walk(ctx context.Context, resolved ResolvedPath, options walkOptions) (walkResult, error) {
	if options.depth < 0 || options.maxEntries < 0 {
		return walkResult{}, fmt.Errorf("%w: depth and max_entries cannot be negative", ErrInvalidRequest)
	}
	depth := options.depth
	if depth <= 0 || depth > m.cfg.MaxDepth {
		depth = m.cfg.MaxDepth
	}
	maxEntries := options.maxEntries
	if maxEntries <= 0 || maxEntries > m.cfg.MaxResults {
		maxEntries = m.cfg.MaxResults
	}
	result := walkResult{entries: make([]Entry, 0)}
	err := filepath.WalkDir(resolved.Absolute, func(current string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == resolved.Absolute {
			return nil
		}
		entryDepth, err := relativeDepth(resolved.Absolute, current)
		if err != nil {
			return err
		}
		if entryDepth > depth {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if m.isIgnored(dirEntry.Name(), options.includeHidden) {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		result.scannedEntries++
		if result.scannedEntries > m.cfg.MaxScannedEntries {
			result.truncated = true
			return filepath.SkipAll
		}
		if dirEntry.Type()&os.ModeSymlink != 0 {
			result.skippedSymlinks++
			return nil
		}
		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(m.resolver.Root(), current)
		if err != nil {
			return err
		}
		result.entries = append(result.entries, makeEntry(filepath.ToSlash(relative), entryDepth, info))
		if len(result.entries) > maxEntries {
			result.entries = result.entries[:maxEntries]
			result.truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return result, err
}

func makeEntry(relative string, depth int, info os.FileInfo) Entry {
	entry := Entry{
		Path: relative, Name: info.Name(), Type: entryType(info), Size: info.Size(),
		Mode: fmt.Sprintf("%04o", info.Mode().Perm()), ModifiedAt: info.ModTime().UTC().Format(timeFormat), Depth: depth,
	}
	if info.Mode().IsRegular() {
		entry.Extension = strings.ToLower(filepath.Ext(info.Name()))
	}
	return entry
}

func entryType(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "folder"
	case info.Mode().IsRegular():
		return "file"
	default:
		return "other"
	}
}

func (m *Manager) isIgnored(name string, includeHidden bool) bool {
	if !includeHidden && strings.HasPrefix(name, ".") {
		return true
	}
	for _, ignored := range m.cfg.IgnoredNames {
		if name == ignored {
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
