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

	"github.com/example/agent-shell-tool/internal/readconfig"
	"github.com/example/agent-shell-tool/internal/readpolicy"
	"github.com/example/agent-shell-tool/internal/readserver"
)

type stdio struct {
	io.Reader
	io.Writer
}

func (stdio) Close() error { return nil }

func main() {
	cfg := readconfig.Load()
	flag.StringVar(&cfg.Transport, "transport", cfg.Transport, "transport: unix or stdio")
	flag.StringVar(&cfg.SocketPath, "socket", cfg.SocketPath, "Unix socket path")
	flag.StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "workspace root")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	service, err := readserver.New(cfg, logger, readpolicy.AllowAll{})
	if err != nil {
		logger.Error("initialize read file tool", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg, service, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("read file tool stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg readconfig.Config, service *readserver.Server, logger *slog.Logger) error {
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
	logger.Info("read file tool listening", "socket", cfg.SocketPath, "workspace", cfg.Workspace)
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

func runStdio(ctx context.Context, service *readserver.Server, conn io.ReadWriteCloser) error {
	serveDone := make(chan struct{})
	go func() { service.ServeConn(conn); close(serveDone) }()
	select {
	case <-ctx.Done():
		service.Shutdown()
		<-service.Done()
		return ctx.Err()
	case <-serveDone:
		service.Shutdown()
		<-service.Done()
		return nil
	case <-service.ShutdownRequested():
		<-service.Done()
		return nil
	}
}

func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path %s", socketPath)
	}
	connection, dialErr := net.DialTimeout("unix", socketPath, 200*time.Millisecond)
	if dialErr == nil {
		connection.Close()
		return fmt.Errorf("socket %s is already active", socketPath)
	}
	return os.Remove(socketPath)
}
