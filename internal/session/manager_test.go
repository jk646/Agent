//go:build linux

package session

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/policy"
)

func TestPersistentSessionKeepsState(t *testing.T) {
	manager := NewManager(Config{DefaultShell: "/bin/bash", IdleTTL: time.Minute, DetachTTL: time.Minute, KillGrace: 100 * time.Millisecond, OutputLimitBytes: 4096, MaxSessions: 2, TempDir: t.TempDir()}, policy.AllowAll{})
	defer manager.Shutdown()
	var mu sync.Mutex
	var output strings.Builder
	emitter := func(method string, params any) error {
		if method != "session.output" {
			return nil
		}
		data := params.(OutputEvent)
		decoded, _ := base64.StdEncoding.DecodeString(data.DataBase64)
		mu.Lock()
		output.Write(decoded)
		mu.Unlock()
		return nil
	}
	opened, err := manager.Open(context.Background(), "owner", OpenParams{SessionID: "s1", Cwd: t.TempDir()}, emitter)
	if err != nil {
		t.Fatal(err)
	}
	if opened.PID <= 0 {
		t.Fatal("invalid pid")
	}
	if result, err := manager.Run(context.Background(), "owner", RunParams{SessionID: "s1", Command: "export AGENT_VALUE=ready; cd /tmp"}, emitter); err != nil || result.ExitCode != 0 {
		t.Fatalf("first run failed: result=%+v err=%v", result, err)
	}
	if result, err := manager.Run(context.Background(), "owner", RunParams{SessionID: "s1", Command: `printf '%s:%s' "$AGENT_VALUE" "$PWD"`}, emitter); err != nil || result.ExitCode != 0 {
		t.Fatalf("second run failed: result=%+v err=%v", result, err)
	}
	mu.Lock()
	combined := output.String()
	mu.Unlock()
	if !strings.Contains(combined, "ready:/tmp") {
		t.Fatalf("state was not preserved: %q", combined)
	}
	if strings.Contains(combined, "__agent_cmd") || strings.Contains(combined, "AGENT_END_") {
		t.Fatalf("internal command wrapper leaked into output: %q", combined)
	}
}

func TestRunRejectsInvalidLimitsBeforeLookup(t *testing.T) {
	manager := NewManager(Config{DefaultShell: "/bin/bash", IdleTTL: time.Minute, DetachTTL: time.Minute, KillGrace: 100 * time.Millisecond, OutputLimitBytes: 4096, MaxSessions: 1, TempDir: t.TempDir()}, policy.AllowAll{})
	defer manager.Shutdown()

	tests := []RunParams{
		{SessionID: "missing", Command: "true", TimeoutMS: -1},
		{SessionID: "missing", Command: "true", OutputLimitBytes: -1},
	}
	for _, params := range tests {
		if _, err := manager.Run(context.Background(), "owner", params, func(string, any) error { return nil }); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("expected validation error for %+v, got %v", params, err)
		}
	}
}

func TestConcurrentOpenRejectsDuplicateSessionID(t *testing.T) {
	manager := NewManager(Config{DefaultShell: "/bin/bash", IdleTTL: time.Minute, DetachTTL: time.Minute, KillGrace: 100 * time.Millisecond, OutputLimitBytes: 4096, MaxSessions: 8, TempDir: t.TempDir()}, policy.AllowAll{})
	defer manager.Shutdown()

	const attempts = 8
	start := make(chan struct{})
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := manager.Open(context.Background(), "owner", OpenParams{SessionID: "shared", Cwd: t.TempDir()}, func(string, any) error { return nil })
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var opened, conflicts int
	for err := range results {
		switch {
		case err == nil:
			opened++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected open error: %v", err)
		}
	}
	if opened != 1 || conflicts != attempts-1 {
		t.Fatalf("opened=%d conflicts=%d", opened, conflicts)
	}
	if manager.Count() != 1 {
		t.Fatalf("unexpected session count: %d", manager.Count())
	}
}
