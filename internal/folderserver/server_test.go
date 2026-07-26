package folderserver

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/folderconfig"
	"github.com/example/agent-shell-tool/internal/folderpolicy"
	"github.com/example/agent-shell-tool/internal/folderreader"
	"github.com/example/agent-shell-tool/internal/protocol"
)

type rpcClient struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

func newRPCClient(t *testing.T, server *Server) *rpcClient {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	go server.ServeConn(serverSide)
	t.Cleanup(func() { _ = clientSide.Close() })
	return &rpcClient{t: t, conn: clientSide, reader: bufio.NewReader(clientSide)}
}

func (c *rpcClient) call(method string, params, result any) *protocol.Error {
	c.t.Helper()
	c.nextID++
	request := map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params}
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
	data, err := json.Marshal(response.Result)
	if err != nil {
		c.t.Fatal(err)
	}
	if err := json.Unmarshal(data, result); err != nil {
		c.t.Fatal(err)
	}
	return nil
}

func TestReadFolderJSONRPC(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "src", "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := folderconfig.Load()
	cfg.Workspace = workspace
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), folderpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)

	var listed folderreader.ListResult
	if rpcErr := client.call("read_folder.list", folderreader.ListParams{Path: ".", Depth: 2, Limit: 10}, &listed); rpcErr != nil {
		t.Fatalf("list failed: %+v", rpcErr)
	}
	if len(listed.Entries) != 3 {
		t.Fatalf("unexpected list: %+v", listed)
	}
	var summary folderreader.SummaryResult
	if rpcErr := client.call("read_folder.summary", folderreader.SummaryParams{Path: ".", Depth: 3}, &summary); rpcErr != nil {
		t.Fatalf("summary failed: %+v", rpcErr)
	}
	if summary.FileCount != 1 || summary.FolderCount != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	var snapshot folderreader.SnapshotResult
	if rpcErr := client.call("read_folder.snapshot", folderreader.SnapshotParams{Path: ".", Depth: 3}, &snapshot); rpcErr != nil {
		t.Fatalf("snapshot failed: %+v", rpcErr)
	}
	if snapshot.Digest == "" || snapshot.EntryCount != 3 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestReadFolderJSONRPCRejectsEscape(t *testing.T) {
	cfg := folderconfig.Load()
	cfg.Workspace = t.TempDir()
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), folderpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)
	rpcErr := client.call("read_folder.tree", folderreader.TreeParams{Path: "../outside"}, nil)
	if rpcErr == nil || rpcErr.Code != protocol.ErrFolderOutsideWorkspace {
		t.Fatalf("expected workspace error, got %+v", rpcErr)
	}
}
