package searchserver

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/filesearch"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/searchconfig"
	"github.com/example/agent-shell-tool/internal/searchpolicy"
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

func TestSearchJSONRPCFindAndContent(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n// TODO test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := searchconfig.Load()
	cfg.Workspace = workspace
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), searchpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)

	var found filesearch.FindResult
	if rpcErr := client.call("search.find", filesearch.FindParams{Pattern: "*.go", Type: "file"}, &found); rpcErr != nil {
		t.Fatalf("find failed: %+v", rpcErr)
	}
	if len(found.Matches) != 1 || found.Matches[0].Path != "src/main.go" {
		t.Fatalf("unexpected find result: %+v", found)
	}
	var content filesearch.ContentResult
	if rpcErr := client.call("search.content", filesearch.ContentParams{Query: "TODO", FilePattern: "*.go"}, &content); rpcErr != nil {
		t.Fatalf("content failed: %+v", rpcErr)
	}
	if len(content.Matches) != 1 || content.Matches[0].Line != 2 {
		t.Fatalf("unexpected content result: %+v", content)
	}
}

func TestSearchJSONRPCRejectsEscape(t *testing.T) {
	cfg := searchconfig.Load()
	cfg.Workspace = t.TempDir()
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), searchpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)
	rpcErr := client.call("search.find", filesearch.FindParams{Path: "../outside"}, nil)
	if rpcErr == nil || rpcErr.Code != protocol.ErrSearchOutsideWorkspace {
		t.Fatalf("expected workspace error, got %+v", rpcErr)
	}
}
