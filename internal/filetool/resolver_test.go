package filetool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolverRejectsWorkspaceEscape(t *testing.T) {
	resolver, err := NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("../outside", true); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace error, got %v", err)
	}
}

func TestResolverRejectsSymlinkEscape(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	resolver, err := NewResolver(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve("link/file.txt", true); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace error, got %v", err)
	}
}
