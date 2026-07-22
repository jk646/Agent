package filereader

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/example/agent-shell-tool/internal/readpolicy"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	workspace := t.TempDir()
	manager, err := NewManager(Config{
		Workspace: workspace, MaxTextBytes: 1 << 20, MaxChunkBytes: 64 << 10,
		MaxHashBytes: 1 << 20, MaxConcurrent: 4, MaxBatchItems: 10,
	}, readpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Shutdown)
	return manager, workspace
}

func TestReadLinesAndText(t *testing.T) {
	manager, workspace := newTestManager(t)
	content := "alpha\n中文 line\nomega\n"
	if err := os.WriteFile(filepath.Join(workspace, "demo.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := manager.Lines(context.Background(), LinesParams{
		Path: "demo.txt", StartLine: 2, EndLine: 3, IncludeLineNumbers: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lines.Content != "2: 中文 line\n3: omega\n" || lines.TotalLines != 3 || lines.Encoding != "utf-8" {
		t.Fatalf("unexpected lines result: %+v", lines)
	}
	text, err := manager.Text(context.Background(), TextParams{Path: "demo.txt", StartChar: 6, EndChar: 8})
	if err != nil {
		t.Fatal(err)
	}
	if text.Content != "中文" || text.StartChar != 6 || text.EndChar != 8 {
		t.Fatalf("unexpected text result: %+v", text)
	}
}

func TestReadUTF16AndBinaryChunk(t *testing.T) {
	manager, workspace := newTestManager(t)
	utf16Data := []byte{0xff, 0xfe}
	for _, unit := range utf16.Encode([]rune("first\r\nsecond\r\n")) {
		pair := make([]byte, 2)
		binary.LittleEndian.PutUint16(pair, unit)
		utf16Data = append(utf16Data, pair...)
	}
	if err := os.WriteFile(filepath.Join(workspace, "utf16.txt"), utf16Data, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := manager.Lines(context.Background(), LinesParams{Path: "utf16.txt", StartLine: 2, EndLine: 2})
	if err != nil {
		t.Fatal(err)
	}
	if lines.Content != "second\r\n" || lines.Encoding != "utf-16le" || lines.Newline != "crlf" {
		t.Fatalf("unexpected UTF-16 result: %+v", lines)
	}
	binaryData := []byte{0, 1, 2, 3, 4, 5}
	if err := os.WriteFile(filepath.Join(workspace, "data.bin"), binaryData, 0o644); err != nil {
		t.Fatal(err)
	}
	chunk, err := manager.Bytes(context.Background(), BytesParams{Path: "data.bin", Offset: 2, Length: 3, IncludeHash: true})
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(binaryData)
	if chunk.DataBase64 != "AgME" || chunk.NextOffset != 5 || chunk.EOF || chunk.SHA256 != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("unexpected binary result: %+v", chunk)
	}
	if _, err := manager.Text(context.Background(), TextParams{Path: "data.bin"}); err != ErrUnsupportedEncoding {
		t.Fatalf("expected encoding error, got %v", err)
	}
}

func TestReadRejectsEscapeAndSymlink(t *testing.T) {
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

func TestBatchRead(t *testing.T) {
	manager, workspace := newTestManager(t)
	if err := os.WriteFile(filepath.Join(workspace, "demo.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Batch(context.Background(), BatchParams{Reads: []ReadRequest{
		{Kind: "stat", Stat: &StatParams{Path: "demo.txt"}},
		{Kind: "lines", Lines: &LinesParams{Path: "demo.txt", StartLine: 2}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || result.Results[1].Lines.Content != "two\n" {
		t.Fatalf("unexpected batch result: %+v", result)
	}
}
