package readserver

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/filereader"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/readconfig"
	"github.com/example/agent-shell-tool/internal/readpolicy"
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

func TestReadJSONRPCLinesBytesAndHash(t *testing.T) {
	workspace := t.TempDir()
	content := []byte("first\nsecond\nthird\n")
	if err := os.WriteFile(filepath.Join(workspace, "demo.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := readconfig.Load()
	cfg.Workspace = workspace
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), readpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)

	var lines filereader.LinesResult
	if rpcErr := client.call("read.lines", filereader.LinesParams{Path: "demo.txt", StartLine: 2, EndLine: 3}, &lines); rpcErr != nil {
		t.Fatalf("lines failed: %+v", rpcErr)
	}
	if lines.Content != "second\nthird\n" || lines.SHA256 == "" {
		t.Fatalf("unexpected lines result: %+v", lines)
	}
	var bytesResult filereader.BytesResult
	if rpcErr := client.call("read.bytes", filereader.BytesParams{Path: "demo.txt", Offset: 0, Length: 5}, &bytesResult); rpcErr != nil {
		t.Fatalf("bytes failed: %+v", rpcErr)
	}
	if bytesResult.DataBase64 != "Zmlyc3Q=" || bytesResult.EOF {
		t.Fatalf("unexpected bytes result: %+v", bytesResult)
	}
	var hash filereader.HashResult
	if rpcErr := client.call("read.hash", filereader.HashParams{Path: "demo.txt"}, &hash); rpcErr != nil {
		t.Fatalf("hash failed: %+v", rpcErr)
	}
	if hash.SHA256 != lines.SHA256 {
		t.Fatalf("hash mismatch: %s != %s", hash.SHA256, lines.SHA256)
	}
}

func TestReadJSONRPCRejectsEscape(t *testing.T) {
	cfg := readconfig.Load()
	cfg.Workspace = t.TempDir()
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), readpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	client := newRPCClient(t, server)
	rpcErr := client.call("read.stat", filereader.StatParams{Path: "../outside"}, nil)
	if rpcErr == nil || rpcErr.Code != protocol.ErrReadOutsideWorkspace {
		t.Fatalf("expected workspace error, got %+v", rpcErr)
	}
}
