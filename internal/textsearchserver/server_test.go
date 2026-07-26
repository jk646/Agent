package textsearchserver

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/textsearch"
	"github.com/example/agent-shell-tool/internal/textsearchconfig"
	"github.com/example/agent-shell-tool/internal/textsearchpolicy"
)

func TestSearchTextRPC(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "sample.txt"), []byte("before\nTODO here\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(textsearchconfig.Config{Workspace: workspace, MaxMessageBytes: 1 << 20, MaxDepth: 10, MaxScannedFiles: 100, MaxFileBytes: 1 << 20, MaxResults: 100, DefaultLimit: 20, MaxMatchesFile: 20, MaxContextLines: 5, MaxConcurrent: 2, MaxBatchItems: 5}, slog.Default(), textsearchpolicy.AllowAll{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	go server.ServeConn(serverSide)
	request := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "search_text.search", "params": map[string]any{"path": ".", "query": "TODO", "context_before": 1, "context_after": 1}}
	if err := json.NewEncoder(clientSide).Encode(request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(clientSide).ReadBytes('\n')
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
	data, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result textsearch.SearchResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Line != 2 || len(result.Matches[0].Before) != 1 || len(result.Matches[0].After) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
