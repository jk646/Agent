package folderreader

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m *Manager) Summary(parent context.Context, params SummaryParams) (SummaryResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return SummaryResult{}, err
	}
	defer release()
	resolved, _, err := m.resolveFolder(ctx, "summary", params.Path)
	if err != nil {
		return SummaryResult{}, err
	}
	walked, err := m.walk(ctx, resolved, walkOptions{depth: params.Depth, includeHidden: params.IncludeHidden, maxEntries: m.cfg.MaxResults})
	if err != nil {
		return SummaryResult{}, err
	}
	result := SummaryResult{
		ReadID: readID, Path: resolved.Relative, Extensions: make(map[string]int),
		ScannedEntries: walked.scannedEntries, SkippedSymlinks: walked.skippedSymlinks, Truncated: walked.truncated,
	}
	rootEmpty, err := m.folderIsEmpty(resolved.Absolute, params.IncludeHidden)
	if err != nil {
		return SummaryResult{}, err
	}
	if rootEmpty {
		result.EmptyFolders++
	}
	for _, entry := range walked.entries {
		if err := ctx.Err(); err != nil {
			return SummaryResult{}, err
		}
		if entry.Depth > result.MaxDepth {
			result.MaxDepth = entry.Depth
		}
		switch entry.Type {
		case "file":
			result.FileCount++
			result.TotalBytes += entry.Size
			result.Extensions[entry.Extension]++
			entryCopy := entry
			if result.LargestFile == nil || entry.Size > result.LargestFile.Size {
				result.LargestFile = &entryCopy
			}
			if result.RecentlyModified == nil || entry.ModifiedAt > result.RecentlyModified.ModifiedAt {
				result.RecentlyModified = &entryCopy
			}
		case "folder":
			result.FolderCount++
			empty, err := m.folderIsEmpty(filepath.Join(m.resolver.Root(), filepath.FromSlash(entry.Path)), params.IncludeHidden)
			if err != nil {
				return SummaryResult{}, err
			}
			if empty {
				result.EmptyFolders++
			}
		default:
			result.OtherCount++
		}
	}
	return result, nil
}

func (m *Manager) folderIsEmpty(folderPath string, includeHidden bool) (bool, error) {
	children, err := os.ReadDir(folderPath)
	if err != nil {
		return false, err
	}
	for _, child := range children {
		if !m.isIgnored(child.Name(), includeHidden) {
			return false, nil
		}
	}
	return true, nil
}

func (m *Manager) Snapshot(parent context.Context, params SnapshotParams) (SnapshotResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return SnapshotResult{}, err
	}
	defer release()
	resolved, _, err := m.resolveFolder(ctx, "snapshot", params.Path)
	if err != nil {
		return SnapshotResult{}, err
	}
	return m.buildSnapshot(ctx, readID, resolved, params)
}

func (m *Manager) buildSnapshot(ctx context.Context, readID string, resolved ResolvedPath, params SnapshotParams) (SnapshotResult, error) {
	if params.Limit < 0 {
		return SnapshotResult{}, fmt.Errorf("%w: limit cannot be negative", ErrInvalidRequest)
	}
	limit := params.Limit
	if limit <= 0 || limit > m.cfg.MaxResults {
		limit = m.cfg.MaxResults
	}
	walked, err := m.walk(ctx, resolved, walkOptions{depth: params.Depth, includeHidden: params.IncludeHidden, maxEntries: limit})
	if err != nil {
		return SnapshotResult{}, err
	}
	entries := walked.entries
	if params.IncludeFileHashes {
		for index := range entries {
			if err := ctx.Err(); err != nil {
				return SnapshotResult{}, err
			}
			if entries[index].Type != "file" {
				continue
			}
			if entries[index].Size > m.cfg.MaxHashBytes {
				entries[index].HashSkipped = true
				continue
			}
			absolute := filepath.Join(m.resolver.Root(), filepath.FromSlash(entries[index].Path))
			entries[index].SHA256, err = hashRegularFile(ctx, absolute, entries[index], m.cfg.MaxHashBytes)
			if err != nil {
				return SnapshotResult{}, err
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	hash := sha256.New()
	for _, entry := range entries {
		writeDigestEntry(hash, entry)
	}
	snapshotID, err := newSnapshotID()
	if err != nil {
		return SnapshotResult{}, err
	}
	return SnapshotResult{
		ReadID: readID, SnapshotID: snapshotID, Path: resolved.Relative,
		Digest: hex.EncodeToString(hash.Sum(nil)), Entries: entries, EntryCount: len(entries),
		ScannedEntries: walked.scannedEntries, SkippedSymlinks: walked.skippedSymlinks, Truncated: walked.truncated,
	}, nil
}

func (m *Manager) Compare(parent context.Context, params CompareParams) (CompareResult, error) {
	readID, ctx, release, err := m.begin(parent, params.ReadID)
	if err != nil {
		return CompareResult{}, err
	}
	defer release()
	if len(params.PreviousEntries) > m.cfg.MaxResults {
		return CompareResult{}, ErrTooLarge
	}
	resolved, _, err := m.resolveFolder(ctx, "compare", params.Path)
	if err != nil {
		return CompareResult{}, err
	}
	current, err := m.buildSnapshot(ctx, readID, resolved, SnapshotParams{
		Path: params.Path, Depth: params.Depth, IncludeHidden: params.IncludeHidden,
		IncludeFileHashes: params.IncludeFileHashes, Limit: m.cfg.MaxResults,
	})
	if err != nil {
		return CompareResult{}, err
	}
	if current.Truncated {
		return CompareResult{}, fmt.Errorf("%w: comparison requires a complete current snapshot", ErrTooLarge)
	}
	previous := make(map[string]Entry, len(params.PreviousEntries))
	for _, entry := range params.PreviousEntries {
		if entry.Path == "" || strings.IndexByte(entry.Path, 0) >= 0 {
			return CompareResult{}, fmt.Errorf("%w: invalid previous entry path", ErrInvalidRequest)
		}
		if _, duplicate := previous[entry.Path]; duplicate {
			return CompareResult{}, fmt.Errorf("%w: duplicate previous entry %s", ErrInvalidRequest, entry.Path)
		}
		previous[entry.Path] = entry
	}
	result := CompareResult{
		ReadID: readID, CurrentDigest: current.Digest, Added: make([]string, 0), Removed: make([]string, 0), Modified: make([]string, 0),
		ScannedEntries: current.ScannedEntries, SkippedSymlinks: current.SkippedSymlinks,
	}
	for _, entry := range current.Entries {
		old, exists := previous[entry.Path]
		if !exists {
			result.Added = append(result.Added, entry.Path)
			continue
		}
		if sameEntryState(old, entry) {
			result.UnchangedCount++
		} else {
			result.Modified = append(result.Modified, entry.Path)
		}
		delete(previous, entry.Path)
	}
	for path := range previous {
		result.Removed = append(result.Removed, path)
	}
	sort.Strings(result.Added)
	sort.Strings(result.Removed)
	sort.Strings(result.Modified)
	return result, nil
}

func sameEntryState(left, right Entry) bool {
	if left.Type != right.Type || left.Size != right.Size || left.Mode != right.Mode || left.ModifiedAt != right.ModifiedAt {
		return false
	}
	if left.SHA256 != "" || right.SHA256 != "" {
		return left.SHA256 == right.SHA256
	}
	return true
}

func hashRegularFile(ctx context.Context, path string, expected Entry, maxBytes int64) (string, error) {
	if expected.Size > maxBytes {
		return "", ErrTooLarge
	}
	handle, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer handle.Close()
	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := handle.Read(buffer)
		if count > 0 {
			total += int64(count)
			if total > maxBytes {
				return "", ErrTooLarge
			}
			_, _ = hash.Write(buffer[:count])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	after, err := handle.Stat()
	if err != nil {
		return "", err
	}
	if total != expected.Size || after.Size() != expected.Size || fmt.Sprintf("%04o", after.Mode().Perm()) != expected.Mode || after.ModTime().UTC().Format(timeFormat) != expected.ModifiedAt {
		return "", fmt.Errorf("%w: file changed while hashing", ErrInvalidRequest)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func newSnapshotID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "folder-snapshot-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}
