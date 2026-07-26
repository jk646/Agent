package filewriter

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/writepolicy"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	workspace := t.TempDir()
	manager, err := NewManager(Config{Workspace: workspace, TempDir: filepath.Join(t.TempDir(), "journal"), MaxFileBytes: 1 << 20, MaxBatchFiles: 10, MaxBatchBytes: 2 << 20, MaxRollbackBytes: 2 << 20, MaxConcurrent: 4, JournalTTL: time.Minute}, writepolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager, workspace
}

func TestCreatePreviewAndRollback(t *testing.T) {
	manager, workspace := newTestManager(t)
	preview, err := manager.Preview(context.Background(), BatchParams{Operations: []Operation{{Kind: "create", Path: "nested/demo.txt", Content: "hello", CreateParents: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || !preview.Preview || len(preview.Files) != 1 || preview.Files[0].Diff == "" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if _, err := os.Stat(filepath.Join(workspace, "nested/demo.txt")); !os.IsNotExist(err) {
		t.Fatalf("preview modified workspace: %v", err)
	}
	result, err := manager.Create(context.Background(), Operation{Path: "nested/demo.txt", Content: "hello", AddNewline: true, CreateParents: true, Mode: "0640"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "nested/demo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" || result.TransactionID == "" || !result.RollbackAvailable {
		t.Fatalf("unexpected create: %+v %q", result, data)
	}
	rolledBack, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: result.TransactionID})
	if err != nil {
		t.Fatal(err)
	}
	if !rolledBack.RolledBack {
		t.Fatalf("unexpected rollback: %+v", rolledBack)
	}
	if _, err := os.Stat(filepath.Join(workspace, "nested/demo.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file remains: %v", err)
	}
}

func TestOverwriteAppendWriteAtAndRestore(t *testing.T) {
	manager, workspace := newTestManager(t)
	target := filepath.Join(workspace, "data.txt")
	if err := os.WriteFile(target, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalSHA := hashBytes([]byte("abcdef"))
	overwritten, err := manager.Overwrite(context.Background(), Operation{Path: "data.txt", Content: "first", ExpectedSHA256: originalSHA})
	if err != nil {
		t.Fatal(err)
	}
	appended, err := manager.Append(context.Background(), Operation{Path: "data.txt", DataBase64: base64.StdEncoding.EncodeToString([]byte("+second")), ExpectedSHA256: overwritten.Files[0].AfterSHA256})
	if err != nil {
		t.Fatal(err)
	}
	written, err := manager.WriteAt(context.Background(), Operation{Path: "data.txt", Offset: 6, Content: "X", ExpectedSHA256: appended.Files[0].AfterSHA256})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "first+Xecond" {
		t.Fatalf("unexpected content %q", data)
	}
	if _, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: written.TransactionID}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "first+second" {
		t.Fatalf("unexpected restored content %q", data)
	}
}

func TestBatchAndStaleProtection(t *testing.T) {
	manager, workspace := newTestManager(t)
	result, err := manager.Batch(context.Background(), BatchParams{Operations: []Operation{{Kind: "create", Path: "a.txt", Content: "a"}, {Kind: "create", Path: "b.bin", DataBase64: "AAEC"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 2 || result.Files[0].Action != "created" {
		t.Fatalf("unexpected batch: %+v", result)
	}
	if _, err := manager.Overwrite(context.Background(), Operation{Path: "a.txt", Content: "bad", ExpectedSHA256: hashBytes([]byte("wrong"))}); err != ErrStaleFile {
		t.Fatalf("expected stale error, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Rollback(context.Background(), RollbackParams{TransactionID: result.TransactionID}); err != ErrRollbackConflict {
		t.Fatalf("expected rollback conflict, got %v", err)
	}
}

func TestRejectsEscapeSymlinkAndSparseOffset(t *testing.T) {
	manager, workspace := newTestManager(t)
	if _, err := manager.Create(context.Background(), Operation{Path: "../escape", Content: "x"}); err != ErrOutsideWorkspace {
		t.Fatalf("expected outside workspace, got %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(workspace, "link")); err == nil {
		if _, err := manager.Create(context.Background(), Operation{Path: "link/file", Content: "x"}); err != ErrSymlink {
			t.Fatalf("expected symlink error, got %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "small.bin"), []byte{1}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WriteAt(context.Background(), Operation{Path: "small.bin", Offset: 5, Content: "x"}); err == nil {
		t.Fatal("expected sparse offset rejection")
	}
	if _, err := manager.WriteAt(context.Background(), Operation{Path: "small.bin", Offset: 5, Content: "x", AllowSparse: true}); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(workspace, "small.bin"))
	if info.Size() != 6 {
		t.Fatalf("unexpected sparse size %d", info.Size())
	}
}
