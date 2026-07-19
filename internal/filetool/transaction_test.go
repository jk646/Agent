package filetool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceDryRunApplyAndRollback(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "demo.txt")
	writeTestFile(t, path, "old value\n")
	manager := newTestManager(t, workspace)
	info, err := manager.Stat(context.Background(), StatParams{Path: "demo.txt", IncludeHash: true})
	if err != nil {
		t.Fatal(err)
	}
	operation := Operation{Kind: "replace", Path: "demo.txt", ExpectedSHA256: info.SHA256, Replacements: []Replacement{{OldText: "old", NewText: "new", ExpectedOccurrences: 1}}}
	dryRun, err := manager.ApplyEdits(context.Background(), ApplyEditsParams{DryRun: true, Changes: []Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Applied || dryRun.TransactionID != "" || dryRun.Diff == "" {
		t.Fatalf("unexpected dry-run result: %+v", dryRun)
	}
	requireContent(t, path, "old value")
	applied, err := manager.ApplyEdits(context.Background(), ApplyEditsParams{Changes: []Operation{operation}})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || !applied.RollbackAvailable || applied.TransactionID == "" {
		t.Fatalf("unexpected apply result: %+v", applied)
	}
	requireContent(t, path, "new value")
	rolledBack, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: applied.TransactionID})
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack.RolledBack {
		t.Fatalf("unexpected rollback result: %+v", rolledBack)
	}
	requireContent(t, path, "old value")
}

func TestBatchCreateCopyMoveDeleteAndRollback(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "source.txt"), "source\n")
	manager := newTestManager(t, workspace)
	info, err := manager.Stat(context.Background(), StatParams{Path: "source.txt", IncludeHash: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Batch(context.Background(), BatchParams{Operations: []Operation{
		{Kind: "mkdir", Path: "archive"},
		{Kind: "create", Path: "created.txt", Content: "created\n"},
		{Kind: "copy", From: "source.txt", To: "copied.txt", ExpectedSHA256: info.SHA256},
		{Kind: "move", From: "copied.txt", To: "archive/moved.txt", ExpectedSHA256: info.SHA256},
	}})
	if err != nil {
		t.Fatal(err)
	}
	requireContent(t, filepath.Join(workspace, "archive", "moved.txt"), "source")
	if _, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: result.TransactionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file still exists after rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "archive")); !os.IsNotExist(err) {
		t.Fatalf("archive still exists after rollback: %v", err)
	}
}

func TestStaleHashRejected(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "demo.txt"), "value\n")
	manager := newTestManager(t, workspace)
	_, err := manager.ApplyEdits(context.Background(), ApplyEditsParams{Changes: []Operation{{Kind: "replace", Path: "demo.txt", ExpectedSHA256: "stale", Replacements: []Replacement{{OldText: "value", NewText: "new"}}}}})
	if !errors.Is(err, ErrStaleFile) {
		t.Fatalf("expected stale file error, got %v", err)
	}
}

func TestDeleteDirectoryAndRollback(t *testing.T) {
	workspace := t.TempDir()
	writeTestFile(t, filepath.Join(workspace, "tree", "nested.txt"), "nested\n")
	manager := newTestManager(t, workspace)
	info, err := manager.Stat(context.Background(), StatParams{Path: "tree", IncludeHash: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Batch(context.Background(), BatchParams{Operations: []Operation{{Kind: "delete", Path: "tree", ExpectedSHA256: info.SHA256, Recursive: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "tree")); !os.IsNotExist(err) {
		t.Fatalf("tree still exists: %v", err)
	}
	if _, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: result.TransactionID}); err != nil {
		t.Fatal(err)
	}
	requireContent(t, filepath.Join(workspace, "tree", "nested.txt"), "nested")
}
