package fileserver

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/fileconfig"
	"github.com/example/agent-shell-tool/internal/filepolicy"
	"github.com/example/agent-shell-tool/internal/filetool"
	"github.com/example/agent-shell-tool/internal/protocol"
)

type rpcTestClient struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

func newRPCTestClient(t *testing.T, server *Server) *rpcTestClient {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	go server.ServeConn(serverSide)
	t.Cleanup(func() { _ = clientSide.Close() })
	return &rpcTestClient{t: t, conn: clientSide, reader: bufio.NewReader(clientSide)}
}

func (c *rpcTestClient) call(method string, params, result any) *protocol.Error {
	c.t.Helper()
	c.nextID++
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID,
		"method":  method,
		"params":  params,
	}
	if err := json.NewEncoder(c.conn).Encode(request); err != nil {
		c.t.Fatal(err)
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		c.t.Fatal(err)
	}
	var response protocol.Response
	if err := json.Unmarshal(line, &response); err != nil {
		c.t.Fatal(err)
	}
	if response.Error != nil || result == nil {
		return response.Error
	}
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, result); err != nil {
		c.t.Fatal(err)
	}
	return nil
}

func TestFileJSONRPCMutationAndRollback(t *testing.T) {
	workspace := t.TempDir()
	cfg := fileconfig.Load()
	cfg.Workspace = workspace
	cfg.TempDir = t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, err := New(cfg, logger, filepolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCTestClient(t, server)

	var created filetool.TransactionResult
	if rpcErr := client.call("file.create", filetool.Operation{
		Path: "notes/demo.txt", Content: "before\n", CreateParents: true,
	}, &created); rpcErr != nil {
		t.Fatalf("create failed: %+v", rpcErr)
	}
	if !created.Applied || created.TransactionID == "" {
		t.Fatalf("unexpected create result: %+v", created)
	}

	var info filetool.FileInfo
	if rpcErr := client.call("file.stat", filetool.StatParams{Path: "notes/demo.txt", IncludeHash: true}, &info); rpcErr != nil {
		t.Fatalf("stat failed: %+v", rpcErr)
	}
	var edited filetool.TransactionResult
	if rpcErr := client.call("file.apply_edits", filetool.ApplyEditsParams{Changes: []filetool.Operation{{
		Kind: "replace", Path: "notes/demo.txt", ExpectedSHA256: info.SHA256,
		Replacements: []filetool.Replacement{{OldText: "before", NewText: "after", ExpectedOccurrences: 1}},
	}}}, &edited); rpcErr != nil {
		t.Fatalf("replace failed: %+v", rpcErr)
	}
	if edited.TransactionID == "" || !edited.RollbackAvailable {
		t.Fatalf("unexpected edit result: %+v", edited)
	}

	var read filetool.ReadResult
	if rpcErr := client.call("file.read", filetool.ReadParams{Path: "notes/demo.txt"}, &read); rpcErr != nil {
		t.Fatalf("read failed: %+v", rpcErr)
	}
	if read.Content != "after\n" {
		t.Fatalf("unexpected edited content %q", read.Content)
	}

	var rolledBack filetool.RollbackResult
	if rpcErr := client.call("file.rollback", filetool.RollbackParams{TransactionID: edited.TransactionID}, &rolledBack); rpcErr != nil {
		t.Fatalf("rollback failed: %+v", rpcErr)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "notes", "demo.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "before\n" {
		t.Fatalf("unexpected rolled-back content %q", content)
	}
}

func TestFileJSONRPCRejectsWorkspaceEscape(t *testing.T) {
	cfg := fileconfig.Load()
	cfg.Workspace = t.TempDir()
	cfg.TempDir = t.TempDir()
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), filepolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCTestClient(t, server)

	rpcErr := client.call("file.read", filetool.ReadParams{Path: "../outside.txt"}, nil)
	if rpcErr == nil || rpcErr.Code != protocol.ErrFileOutsideWorkspace {
		t.Fatalf("expected outside-workspace error, got %+v", rpcErr)
	}
}
