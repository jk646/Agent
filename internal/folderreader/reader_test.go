package folderreader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/folderpolicy"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	workspace := t.TempDir()
	writeFile(t, workspace, "README.md", "read me\n")
	writeFile(t, workspace, "src/main.go", "package main\n")
	writeFile(t, workspace, "src/lib/util.go", "package lib\n")
	writeFile(t, workspace, ".hidden/secret.txt", "secret\n")
	if err := os.MkdirAll(filepath.Join(workspace, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(Config{
		Workspace: workspace, MaxDepth: 10, MaxScannedEntries: 1000, MaxResults: 100,
		DefaultLimit: 20, MaxHashBytes: 1 << 20, MaxConcurrent: 4, MaxBatchItems: 10,
	}, folderpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager, workspace
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatListAndTree(t *testing.T) {
	manager, workspace := newTestManager(t)
	if err := os.Symlink(filepath.Join(workspace, "README.md"), filepath.Join(workspace, "readme-link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	stat, err := manager.Stat(context.Background(), StatParams{Path: ".", IncludeDigest: true})
	if err != nil {
		t.Fatal(err)
	}
	if stat.FileCount != 1 || stat.FolderCount != 2 || stat.OtherCount != 1 || stat.Digest == "" {
		t.Fatalf("unexpected stat: %+v", stat)
	}
	filesOnly := false
	foldersOnly := false
	listed, err := manager.List(context.Background(), ListParams{
		Path: ".", Depth: 3, IncludeFiles: &filesOnly, IncludeFolders: &foldersOnly,
		Extensions: []string{"go"}, SortBy: "path", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("both filters false should return no entries: %+v", listed.Entries)
	}
	includeFiles := true
	excludeFolders := false
	listed, err = manager.List(context.Background(), ListParams{
		Path: ".", Depth: 3, IncludeFiles: &includeFiles, IncludeFolders: &excludeFolders,
		Extensions: []string{"go"}, SortBy: "path", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 2 || listed.Entries[0].Path != "src/lib/util.go" || listed.SkippedSymlinks != 1 {
		t.Fatalf("unexpected list: %+v", listed)
	}
	tree, err := manager.Tree(context.Background(), TreeParams{Path: ".", Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root.Name != "." || tree.EntryCount != 5 || tree.Truncated {
		t.Fatalf("unexpected tree: %+v", tree)
	}
}

func TestSummarySnapshotAndCompare(t *testing.T) {
	manager, workspace := newTestManager(t)
	summary, err := manager.Summary(context.Background(), SummaryParams{Path: ".", Depth: 10})
	if err != nil {
		t.Fatal(err)
	}
	if summary.FileCount != 3 || summary.FolderCount != 3 || summary.EmptyFolders != 1 || summary.Extensions[".go"] != 2 || summary.MaxDepth != 3 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	snapshot, err := manager.Snapshot(context.Background(), SnapshotParams{Path: ".", Depth: 10, IncludeFileHashes: true})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest == "" || snapshot.EntryCount != 6 || snapshot.Truncated {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	writeFile(t, workspace, "README.md", "changed content\n")
	if err := os.Remove(filepath.Join(workspace, "src", "lib", "util.go")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "src/new.go", "package src\n")
	compared, err := manager.Compare(context.Background(), CompareParams{
		Path: ".", Depth: 10, IncludeFileHashes: true, PreviousEntries: snapshot.Entries,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compared.Added) != 1 || compared.Added[0] != "src/new.go" {
		t.Fatalf("unexpected added entries: %+v", compared)
	}
	if len(compared.Removed) != 1 || compared.Removed[0] != "src/lib/util.go" {
		t.Fatalf("unexpected removed entries: %+v", compared)
	}
	if len(compared.Modified) != 3 || compared.Modified[0] != "README.md" || compared.Modified[1] != "src" || compared.Modified[2] != "src/lib" {
		t.Fatalf("unexpected modified entries: %+v", compared)
	}
}

func TestRejectsEscapeAndNonFolder(t *testing.T) {
	manager, _ := newTestManager(t)
	if _, err := manager.Stat(context.Background(), StatParams{Path: "../outside"}); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace error, got %v", err)
	}
	if _, err := manager.Stat(context.Background(), StatParams{Path: "README.md"}); err != ErrNotFolder {
		t.Fatalf("expected not-folder error, got %v", err)
	}
}
