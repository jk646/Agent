package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/agent-shell-tool/internal/orchestratorconfig"
	"github.com/example/agent-shell-tool/internal/orchestratorserver"
)

type stdio struct {
	io.Reader
	io.Writer
}

func (stdio) Close() error { return nil }

func main() {
	cfg := orchestratorconfig.Load()
	flag.StringVar(&cfg.Transport, "transport", cfg.Transport, "transport: unix or stdio")
	flag.StringVar(&cfg.SocketPath, "socket", cfg.SocketPath, "orchestrator Unix socket")
	flag.StringVar(&cfg.ToolsFile, "tools-config", cfg.ToolsFile, "tool registry JSON file")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	service, err := orchestratorserver.New(cfg, logger)
	if err != nil {
		logger.Error("initialize orchestrator", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, service, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("orchestrator stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg orchestratorconfig.Config, service *orchestratorserver.Server, logger *slog.Logger) error {
	if cfg.Transport == "stdio" {
		return runStdio(ctx, service, stdio{Reader: os.Stdin, Writer: os.Stdout})
	}
	if cfg.Transport != "unix" {
		return fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o750); err != nil {
		return err
	}
	if err := removeStaleSocket(cfg.SocketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(cfg.SocketPath)
	if err := os.Chmod(cfg.SocketPath, 0o660); err != nil {
		return err
	}
	logger.Info("orchestrator listening", "socket", cfg.SocketPath, "tools_config", cfg.ToolsFile)
	go func() {
		select {
		case <-ctx.Done():
		case <-service.ShutdownRequested():
		}
		_ = listener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				service.Shutdown()
				<-service.Done()
				return ctx.Err()
			case <-service.ShutdownRequested():
				<-service.Done()
				return nil
			default:
				return err
			}
		}
		go service.ServeConn(connection)
	}
}
func runStdio(ctx context.Context, service *orchestratorserver.Server, conn io.ReadWriteCloser) error {
	done := make(chan struct{})
	go func() { service.ServeConn(conn); close(done) }()
	select {
	case <-ctx.Done():
		service.Shutdown()
		<-service.Done()
		return ctx.Err()
	case <-done:
		service.Shutdown()
		<-service.Done()
		return nil
	case <-service.ShutdownRequested():
		<-service.Done()
		return nil
	}
}
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path %s", path)
	}
	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		return fmt.Errorf("socket %s is already active", path)
	}
	return os.Remove(path)
}
