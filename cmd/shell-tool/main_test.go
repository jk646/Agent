//go:build linux

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/example/agent-shell-tool/internal/config"
	"github.com/example/agent-shell-tool/internal/policy"
	"github.com/example/agent-shell-tool/internal/server"
)

func TestRunStdioReturnsWhenInputCloses(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	service := server.New(config.Config{
		DefaultShell:     "/bin/bash",
		DefaultTimeout:   time.Second,
		KillGrace:        10 * time.Millisecond,
		SessionIdleTTL:   time.Minute,
		SessionDetachTTL: time.Minute,
		ShutdownGrace:    10 * time.Millisecond,
		OutputLimitBytes: 1024,
		MaxMessageBytes:  1024,
		MaxExec:          1,
		MaxSessions:      1,
		TempDir:          t.TempDir(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), policy.AllowAll{})

	done := make(chan error, 1)
	go func() { done <- runStdio(context.Background(), service, serverSide) }()
	if err := clientSide.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stdio server did not stop after EOF")
	}
}
