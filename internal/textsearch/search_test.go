package textsearch

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/example/agent-shell-tool/internal/textsearchpolicy"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	workspace := t.TempDir()
	writeFile(t, workspace, "main.go", "package main\n// TODO root\n")
	writeFile(t, workspace, "internal/app.go", "package internal\n// alpha TODO beta\n// todo again\n")
	writeFile(t, workspace, "docs/note.txt", "TODO docs\n")
	writeFile(t, workspace, ".hidden/secret.go", "TODO hidden\n")
	writeFile(t, workspace, "node_modules/pkg.go", "TODO ignored\n")
	writeFile(t, workspace, "binary.dat", string([]byte{0, 1, 2}))
	manager, err := NewManager(Config{Workspace: workspace, MaxDepth: 20, MaxScannedFiles: 1000, MaxFileBytes: 1 << 20, MaxResults: 100, DefaultLimit: 20, MaxMatchesFile: 20, MaxContextLines: 5, MaxConcurrent: 4, MaxBatchItems: 10, IgnoredNames: []string{"node_modules"}}, textsearchpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager, workspace
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchLiteralFiltersContextAndPagination(t *testing.T) {
	manager, _ := newTestManager(t)
	result, err := manager.Search(context.Background(), SearchParams{Path: ".", Query: "TODO", CaseSensitive: true, IncludePatterns: []string{"**/*.go"}, ExcludePatterns: []string{"main.go"}, ContextBefore: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "internal/app.go" || result.Matches[0].Line != 2 || result.Matches[0].Column != 10 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Matches[0].Before) != 1 || result.NextCursor != 0 {
		t.Fatalf("unexpected pagination/context: %+v", result)
	}
	if result.SkippedBinary != 0 {
		t.Fatalf("filtered binary should not be scanned: %+v", result)
	}
}

func TestRegexMultiWholeWordAndCount(t *testing.T) {
	manager, _ := newTestManager(t)
	result, err := manager.Multi(context.Background(), MultiParams{SearchParams: SearchParams{Path: ".", Extensions: []string{"go"}}, Patterns: []Pattern{{ID: "todo", Query: "todo", WholeWord: true}, {ID: "package", Query: `^package\s+\w+`, Regex: true, CaseSensitive: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 5 {
		t.Fatalf("expected five matches, got %+v", result.Matches)
	}
	count, err := manager.Count(context.Background(), SearchParams{Path: ".", Query: "TODO", CaseSensitive: false, Extensions: []string{".go"}})
	if err != nil {
		t.Fatal(err)
	}
	if count.Total != 3 || len(count.Files) != 2 {
		t.Fatalf("unexpected count: %+v", count)
	}
}

func TestUTF16OffsetAndUnicodeColumn(t *testing.T) {
	manager, workspace := newTestManager(t)
	text := "α TODO\r\n"
	units := utf16.Encode([]rune(text))
	data := []byte{0xff, 0xfe}
	for _, unit := range units {
		pair := make([]byte, 2)
		binary.LittleEndian.PutUint16(pair, unit)
		data = append(data, pair...)
	}
	if err := os.WriteFile(filepath.Join(workspace, "utf16.txt"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Search(context.Background(), SearchParams{Path: "utf16.txt", Query: "TODO", CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("unexpected matches: %+v", result)
	}
	match := result.Matches[0]
	if match.Column != 3 || match.ByteOffset != 6 || match.Text != "α TODO" {
		t.Fatalf("unexpected UTF-16 location: %+v", match)
	}
}

func TestInvertFilesAndWorkspaceBoundary(t *testing.T) {
	manager, workspace := newTestManager(t)
	result, err := manager.Files(context.Background(), SearchParams{Path: "docs", Query: "missing", InvertMatch: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "docs/note.txt" {
		t.Fatalf("unexpected paths: %+v", result)
	}
	if _, err := manager.Search(context.Background(), SearchParams{Path: "../outside", Query: "x"}); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace, got %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(workspace, "link")); err == nil {
		if _, err := manager.Search(context.Background(), SearchParams{Path: "link", Query: "x"}); err != ErrSymlink {
			t.Fatalf("expected symlink error, got %v", err)
		}
	}
}

func TestPerFileLimitAndCountPagination(t *testing.T) {
	manager, _ := newTestManager(t)
	result, err := manager.Search(context.Background(), SearchParams{Path: "internal/app.go", Query: "todo", MaxMatchesPerFile: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || !result.Truncated {
		t.Fatalf("expected per-file truncation: %+v", result)
	}
	count, err := manager.Count(context.Background(), SearchParams{Path: ".", Query: "TODO", Extensions: []string{"go"}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(count.Files) != 1 || count.NextCursor != 1 || !count.Truncated || count.Total != 3 {
		t.Fatalf("unexpected count page: %+v", count)
	}
}
