package writeserver

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/filewriter"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/writeconfig"
	"github.com/example/agent-shell-tool/internal/writepolicy"
)

func TestWriteFileRPC(t *testing.T) {
	workspace := t.TempDir()
	server, err := New(writeconfig.Config{Workspace: workspace, TempDir: filepath.Join(t.TempDir(), "journal"), MaxMessageBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxBatchFiles: 10, MaxBatchBytes: 2 << 20, MaxRollbackBytes: 2 << 20, MaxConcurrent: 2, JournalTTL: time.Minute, ShutdownGrace: time.Second}, slog.Default(), writepolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	go server.ServeConn(serverSide)
	result := call(t, clientSide, "write_file.create", map[string]any{"path": "nested/demo.txt", "content": "hello\n", "create_parents": true})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var writeResult filewriter.Result
	if err := json.Unmarshal(data, &writeResult); err != nil {
		t.Fatal(err)
	}
	if !writeResult.Applied || writeResult.TransactionID == "" {
		t.Fatalf("unexpected result: %+v", writeResult)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "nested/demo.txt"))
	if err != nil || string(content) != "hello\n" {
		t.Fatalf("unexpected file: %q %v", content, err)
	}
	rollback := call(t, clientSide, "write_file.rollback", map[string]any{"transaction_id": writeResult.TransactionID})
	data, _ = json.Marshal(rollback)
	var rolledBack filewriter.RollbackResult
	if err := json.Unmarshal(data, &rolledBack); err != nil {
		t.Fatal(err)
	}
	if !rolledBack.RolledBack {
		t.Fatalf("unexpected rollback: %+v", rolledBack)
	}
}

func call(t *testing.T, connection net.Conn, method string, params any) any {
	t.Helper()
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response protocol.Response
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil {
		t.Fatalf("RPC error: %+v", response.Error)
	}
	return response.Result
}
