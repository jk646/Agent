package filetool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/agent-shell-tool/internal/filepolicy"
)

func TestReadListAndSearch(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "alpha.txt"), "first\nneedle line\nthird\n")
	writeTestFile(t, filepath.Join(workspace, ".hidden"), "needle hidden\n")
	manager := newTestManager(t, workspace)
	read, err := manager.Read(context.Background(), ReadParams{Path: "alpha.txt", StartLine: 2, EndLine: 2})
	if err != nil {
		t.Fatal(err)
	}
	if read.Content != "needle line\n" || read.Newline != "lf" || read.SHA256 == "" {
		t.Fatalf("unexpected read result: %+v", read)
	}
	listed, err := manager.List(context.Background(), ListParams{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 1 || listed.Entries[0].Path != "alpha.txt" {
		t.Fatalf("unexpected list: %+v", listed)
	}
	searched, err := manager.Search(context.Background(), SearchParams{Query: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched.Matches) != 1 || searched.Matches[0].Line != 2 {
		t.Fatalf("unexpected search: %+v", searched)
	}
}

func TestFindUsesGlob(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "a.go"), "package a\n")
	writeTestFile(t, filepath.Join(workspace, "b.txt"), "text\n")
	manager := newTestManager(t, workspace)
	result, err := manager.Find(context.Background(), FindParams{Pattern: "*.go", Type: "file"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Path != "a.go" {
		t.Fatalf("unexpected find result: %+v", result)
	}
}

func newTestManager(t *testing.T, workspace string) *Manager {
	t.Helper()
	manager, err := NewManager(Config{
		Workspace: workspace, TempDir: filepath.Join(t.TempDir(), "journal"), MaxFileBytes: 1 << 20,
		MaxReadBytes: 64 << 10, MaxEntries: 100, MaxSearchMatches: 100,
		MaxTransactionFiles: 100, MaxTransactionBytes: 8 << 20, MaxDiffBytes: 1 << 20, MaxConcurrent: 4,
	}, filepolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireContent(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != strings.TrimSpace(expected) {
		t.Fatalf("unexpected content %q", data)
	}
}
