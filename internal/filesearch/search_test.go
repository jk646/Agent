package filesearch

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/searchpolicy"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	workspace := t.TempDir()
	writeTestFile(t, workspace, "cmd/app/main.go", "package main\n\n// TODO: wire server\nfunc main() {}\n")
	writeTestFile(t, workspace, "internal/server/server.go", "package server\n\nfunc Start() {}\n")
	writeTestFile(t, workspace, "README.md", "Agent search tool\n")
	writeTestFile(t, workspace, ".hidden/secret.go", "TODO hidden\n")
	writeTestFile(t, workspace, "node_modules/pkg/index.go", "TODO ignored\n")
	writeTestFile(t, workspace, "binary.dat", string([]byte{0, 1, 2}))
	manager, err := NewManager(Config{
		Workspace: workspace, MaxFileBytes: 1 << 20, MaxResults: 100,
		MaxScannedEntries: 1000, MaxDepth: 20, MaxConcurrent: 4,
		IgnoredNames: []string{"node_modules"},
	}, searchpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager, workspace
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindFiltersAndRanks(t *testing.T) {
	manager, _ := newTestManager(t)
	result, err := manager.Find(context.Background(), FindParams{
		Path: ".", Name: "server", Type: "file", Extensions: []string{"go"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "internal/server/server.go" {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	if result.ScannedEntries == 0 || result.SearchID == "" {
		t.Fatalf("missing search metadata: %+v", result)
	}
}

func TestContentSearchContextAndIgnoredFiles(t *testing.T) {
	manager, _ := newTestManager(t)
	result, err := manager.Content(context.Background(), ContentParams{
		Path: ".", Query: "todo", FilePattern: "*.go", ContextBefore: 1, ContextAfter: 1, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	match := result.Matches[0]
	if match.Path != "cmd/app/main.go" || match.Line != 3 || match.Column != 4 {
		t.Fatalf("unexpected match: %+v", match)
	}
	if len(match.Before) != 1 || len(match.After) != 1 {
		t.Fatalf("unexpected context: %+v", match)
	}
}

func TestResolverRejectsEscapeAndSymlink(t *testing.T) {
	manager, workspace := newTestManager(t)
	if _, err := manager.Stat(context.Background(), StatParams{Path: "../outside"}); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace error, got %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := manager.Stat(context.Background(), StatParams{Path: "link"}); err != ErrSymlink {
		t.Fatalf("expected symlink error, got %v", err)
	}
}
