package orchestrator

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/protocol"
)

type mockTool struct {
	listener net.Listener
	done     chan struct{}
}

func startMockTool(t *testing.T, socket, name string) *mockTool {
	t.Helper()
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	tool := &mockTool{listener: listener, done: make(chan struct{})}
	go func() {
		defer close(tool.done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go serveMock(connection, name)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-tool.done })
	return tool
}
func serveMock(raw net.Conn, name string) {
	conn := protocol.NewConn(raw, 1<<20)
	defer conn.Close()
	for {
		request, err := conn.ReadRequest()
		if err != nil {
			return
		}
		switch request.Method {
		case "system.initialize", "system.capabilities":
			_ = conn.Respond(protocol.NewResponse(request.ID, map[string]any{"name": name, "protocol_version": "1"}))
		case "system.health":
			_ = conn.Respond(protocol.NewResponse(request.ID, map[string]any{"status": "ok", "name": name}))
		case "alpha.echo", "beta.echo":
			var params any
			_ = json.Unmarshal(request.Params, &params)
			_ = conn.Respond(protocol.NewResponse(request.ID, map[string]any{"tool": name, "params": params}))
			_ = conn.Notify("mock.output", map[string]string{"tool": name})
		default:
			_ = conn.Respond(protocol.NewError(request.ID, protocol.MethodNotFound, "method not found", nil))
		}
	}
}

func writeToolsConfig(t *testing.T, path string, tools []ToolSpec) {
	t.Helper()
	data, err := json.Marshal(ToolsConfig{ProtocolVersion: "1", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRoutesCallsEventsAndReloads(t *testing.T) {
	directory := t.TempDir()
	alphaSocket := filepath.Join(directory, "alpha.sock")
	betaSocket := filepath.Join(directory, "beta.sock")
	startMockTool(t, alphaSocket, "alpha")
	startMockTool(t, betaSocket, "beta")
	configPath := filepath.Join(directory, "tools.json")
	alpha := ToolSpec{Name: "alpha", Socket: alphaSocket, MethodPrefixes: []string{"alpha."}, Required: true}
	writeToolsConfig(t, configPath, []ToolSpec{alpha})
	registry, err := NewRegistry(configPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if tool, err := registry.Resolve("alpha.echo"); err != nil || tool != "alpha" {
		t.Fatalf("unexpected route %q %v", tool, err)
	}
	events, unsubscribe := registry.Subscribe()
	defer unsubscribe()
	params := json.RawMessage(`{"value":42}`)
	result, err := registry.Call(context.Background(), CallParams{Method: "alpha.echo", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(result.Result, &response); err != nil {
		t.Fatal(err)
	}
	if response["tool"] != "alpha" {
		t.Fatalf("unexpected response: %+v", response)
	}
	select {
	case event := <-events:
		if event.Tool != "alpha" || event.Method != "mock.output" {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tool event")
	}
	beta := ToolSpec{Name: "beta", Socket: betaSocket, MethodPrefixes: []string{"beta."}}
	writeToolsConfig(t, configPath, []ToolSpec{alpha, beta})
	if err := registry.Reload(); err != nil {
		t.Fatal(err)
	}
	if tool, err := registry.Resolve("beta.echo"); err != nil || tool != "beta" {
		t.Fatalf("new tool was not loaded: %q %v", tool, err)
	}
	if _, err := registry.Call(context.Background(), CallParams{Method: "beta.echo", Params: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestEngineParallelBatch(t *testing.T) {
	directory := t.TempDir()
	socket := filepath.Join(directory, "alpha.sock")
	startMockTool(t, socket, "alpha")
	configPath := filepath.Join(directory, "tools.json")
	writeToolsConfig(t, configPath, []ToolSpec{{Name: "alpha", Socket: socket, MethodPrefixes: []string{"alpha."}}})
	registry, err := NewRegistry(configPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	engine := NewEngine(registry, 4)
	result, err := engine.Batch(context.Background(), BatchParams{Parallel: true, Calls: []CallParams{{Method: "alpha.echo", Params: json.RawMessage(`{"id":1}`)}, {Tool: "alpha", Method: "system.health"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || result.Results[0].Result == nil || result.Results[1].Result == nil {
		t.Fatalf("unexpected batch: %+v", result)
	}
}

func TestRejectsAmbiguousPrefixes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tools.json")
	writeToolsConfig(t, configPath, []ToolSpec{{Name: "one", Socket: "/tmp/one.sock", MethodPrefixes: []string{"shared."}}, {Name: "two", Socket: "/tmp/two.sock", MethodPrefixes: []string{"shared."}}})
	registry, err := NewRegistry(configPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, err := registry.Resolve("shared.call"); err != ErrMethodNotRoutable {
		t.Fatalf("expected ambiguous route error, got %v", err)
	}
}
