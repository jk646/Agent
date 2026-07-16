//go:build linux

package executor

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/output"
	"github.com/example/agent-shell-tool/internal/policy"
)

func TestManagerStreamsAndReturnsExitCode(t *testing.T) {
	manager := newTestManager(t)
	var mu sync.Mutex
	var combined strings.Builder
	exited := make(chan ExitedEvent, 1)
	emitter := func(method string, params any) error {
		switch method {
		case "exec.output":
			event := params.(output.ChunkEvent)
			data, _ := base64.StdEncoding.DecodeString(event.DataBase64)
			mu.Lock()
			combined.Write(data)
			mu.Unlock()
		case "exec.exited":
			exited <- params.(ExitedEvent)
		}
		return nil
	}
	result, err := manager.Start(context.Background(), "owner", StartParams{RequestID: "test", Command: "printf out; printf err >&2; exit 7"}, emitter)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted {
		t.Fatal("execution not accepted")
	}
	event := waitExited(t, exited)
	if event.ExitCode != 7 {
		t.Fatalf("unexpected exit code: %d", event.ExitCode)
	}
	mu.Lock()
	text := combined.String()
	mu.Unlock()
	if !strings.Contains(text, "out") || !strings.Contains(text, "err") {
		t.Fatalf("missing output: %q", text)
	}
}

func TestManagerImmediateCancel(t *testing.T) {
	manager := newTestManager(t)
	exited := make(chan ExitedEvent, 1)
	result, err := manager.Start(context.Background(), "owner", StartParams{RequestID: "cancel", Command: "sleep 30"}, func(method string, params any) error {
		if method == "exec.exited" {
			exited <- params.(ExitedEvent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Cancel(result.RequestID); err != nil {
		t.Fatal(err)
	}
	event := waitExited(t, exited)
	if !event.Canceled {
		t.Fatalf("expected canceled event: %+v", event)
	}
}

func TestManagerTimeout(t *testing.T) {
	manager := newTestManager(t)
	exited := make(chan ExitedEvent, 1)
	_, err := manager.Start(context.Background(), "owner", StartParams{RequestID: "timeout", Command: "sleep 30", TimeoutMS: 25}, func(method string, params any) error {
		if method == "exec.exited" {
			exited <- params.(ExitedEvent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	event := waitExited(t, exited)
	if !event.TimedOut {
		t.Fatalf("expected timeout event: %+v", event)
	}
}

func TestManagerWritesToStdin(t *testing.T) {
	manager := newTestManager(t)
	exited := make(chan ExitedEvent, 1)
	var mu sync.Mutex
	var outputText strings.Builder
	_, err := manager.Start(context.Background(), "owner", StartParams{RequestID: "stdin", Command: "cat", EnableStdin: true}, func(method string, params any) error {
		switch method {
		case "exec.started":
			if err := manager.Write("stdin", []byte("hello stdin"), true); err != nil {
				t.Errorf("write stdin: %v", err)
			}
		case "exec.output":
			event := params.(output.ChunkEvent)
			data, _ := base64.StdEncoding.DecodeString(event.DataBase64)
			mu.Lock()
			outputText.Write(data)
			mu.Unlock()
		case "exec.exited":
			exited <- params.(ExitedEvent)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	event := waitExited(t, exited)
	if event.ExitCode != 0 {
		t.Fatalf("unexpected exit: %+v", event)
	}
	mu.Lock()
	got := outputText.String()
	mu.Unlock()
	if got != "hello stdin" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestValidateRejectsOverflowingTimeout(t *testing.T) {
	err := Validate(StartParams{Command: "true", TimeoutMS: 1<<63 - 1})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(Config{DefaultShell: "/bin/bash", DefaultTimeout: time.Second, KillGrace: 50 * time.Millisecond, OutputLimitBytes: 1024, MaxConcurrent: 2, TempDir: t.TempDir()}, policy.AllowAll{})
}

func waitExited(t *testing.T, exited <-chan ExitedEvent) ExitedEvent {
	t.Helper()
	select {
	case event := <-exited:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for exit")
		return ExitedEvent{}
	}
}
