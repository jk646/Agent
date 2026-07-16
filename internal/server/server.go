package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/config"
	"github.com/example/agent-shell-tool/internal/executor"
	"github.com/example/agent-shell-tool/internal/output"
	"github.com/example/agent-shell-tool/internal/policy"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/session"
)

type Server struct {
	cfg          config.Config
	logger       *slog.Logger
	executor     *executor.Manager
	sessions     *session.Manager
	shutdown     chan struct{}
	done         chan struct{}
	shutdownOnce sync.Once
	startedAt    time.Time
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocol_version"`
	ClientInfo      map[string]any `json:"client_info,omitempty"`
}
type Capabilities struct {
	ProtocolVersion string   `json:"protocol_version"`
	PTY             bool     `json:"pty"`
	Resize          bool     `json:"resize"`
	Signals         []string `json:"signals"`
	Shells          []string `json:"shells"`
	Transports      []string `json:"transports"`
}
type Health struct {
	Status           string `json:"status"`
	UptimeMS         int64  `json:"uptime_ms"`
	ActiveExecutions int    `json:"active_executions"`
	ActiveSessions   int    `json:"active_sessions"`
}

func New(cfg config.Config, logger *slog.Logger, executionPolicy policy.ExecutionPolicy) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, logger: logger, shutdown: make(chan struct{}), done: make(chan struct{}), startedAt: time.Now(), executor: executor.NewManager(executor.Config{DefaultShell: cfg.DefaultShell, DefaultTimeout: cfg.DefaultTimeout, KillGrace: cfg.KillGrace, OutputLimitBytes: cfg.OutputLimitBytes, MaxConcurrent: cfg.MaxExec, TempDir: cfg.TempDir}, executionPolicy), sessions: session.NewManager(session.Config{DefaultShell: cfg.DefaultShell, DefaultTimeout: cfg.DefaultTimeout, IdleTTL: cfg.SessionIdleTTL, DetachTTL: cfg.SessionDetachTTL, KillGrace: cfg.KillGrace, OutputLimitBytes: cfg.OutputLimitBytes, MaxSessions: cfg.MaxSessions, TempDir: cfg.TempDir}, executionPolicy)}
}
func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }
func (s *Server) Done() <-chan struct{}              { return s.done }
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdown)
		deadline := time.Now().Add(s.cfg.ShutdownGrace)
		for time.Now().Before(deadline) {
			if s.executor.Count() == 0 && s.sessions.Count() == 0 {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		s.executor.Shutdown()
		s.sessions.Shutdown()
		close(s.done)
	})
}

func (s *Server) ServeConn(raw io.ReadWriteCloser) {
	conn := protocol.NewConn(raw, s.cfg.MaxMessageBytes)
	owner := fmt.Sprintf("%s-%p", conn.Identity(), conn)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		s.executor.CancelOwner(owner)
		s.sessions.DetachOwner(owner)
		_ = conn.Close()
		s.logger.Info("client disconnected", "owner", owner)
	}()
	s.logger.Info("client connected", "owner", owner)
	for {
		request, err := conn.ReadRequest()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = conn.Respond(protocol.NewError(nil, protocol.ParseError, "invalid request", err.Error()))
			if errors.Is(err, protocol.ErrMalformedRequest) {
				continue
			}
			return
		}
		go s.handle(ctx, owner, conn, request)
	}
}

func (s *Server) handle(ctx context.Context, owner string, conn *protocol.Conn, request protocol.Request) {
	emitter := output.Emitter(func(method string, params any) error { return conn.Notify(method, params) })
	respond := func(result any, err error) {
		if len(request.ID) == 0 {
			return
		}
		if err == nil {
			_ = conn.Respond(protocol.NewResponse(request.ID, result))
			return
		}
		code := errorCode(err)
		_ = conn.Respond(protocol.NewError(request.ID, code, err.Error(), nil))
	}
	switch request.Method {
	case "system.initialize":
		var params InitializeParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		if params.ProtocolVersion != "" && params.ProtocolVersion != protocol.Version {
			respond(nil, fmt.Errorf("unsupported protocol version %q", params.ProtocolVersion))
			return
		}
		respond(s.capabilities(), nil)
	case "system.capabilities":
		respond(s.capabilities(), nil)
	case "system.health":
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveExecutions: s.executor.Count(), ActiveSessions: s.sessions.Count()}, nil)
	case "system.shutdown":
		respond(map[string]any{"accepted": true}, nil)
		go s.Shutdown()
	case "exec.start":
		var params executor.StartParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		result, err := s.executor.Start(ctx, owner, params, emitter)
		respond(result, err)
	case "exec.cancel":
		var params executor.CancelParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		err := s.executor.Cancel(params.RequestID)
		respond(map[string]any{"canceled": err == nil}, err)
	case "exec.write":
		var params executor.WriteParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		data, err := executor.DecodeInput(params.DataBase64)
		if err == nil {
			err = s.executor.Write(params.RequestID, data, params.Close)
		}
		respond(map[string]any{"written": err == nil}, err)
	case "session.open":
		var params session.OpenParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		result, err := s.sessions.Open(ctx, owner, params, emitter)
		respond(result, err)
	case "session.run":
		var params session.RunParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		result, err := s.sessions.Run(ctx, owner, params, emitter)
		respond(result, err)
	case "session.write":
		var params session.WriteParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		err := s.sessions.Write(owner, params, emitter)
		respond(map[string]any{"written": err == nil}, err)
	case "session.resize":
		var params session.ResizeParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		err := s.sessions.Resize(owner, params, emitter)
		respond(map[string]any{"resized": err == nil}, err)
	case "session.interrupt":
		var params session.IDParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		err := s.sessions.Interrupt(owner, params, emitter)
		respond(map[string]any{"interrupted": err == nil}, err)
	case "session.close":
		var params session.IDParams
		if err := decode(request.Params, &params); err != nil {
			respond(nil, err)
			return
		}
		err := s.sessions.Close(params.SessionID)
		respond(map[string]any{"closed": err == nil}, err)
	case "session.list":
		respond(s.sessions.List(), nil)
	default:
		if len(request.ID) > 0 {
			_ = conn.Respond(protocol.NewError(request.ID, protocol.MethodNotFound, "method not found", request.Method))
		}
	}
}

func (s *Server) capabilities() Capabilities {
	return Capabilities{ProtocolVersion: protocol.Version, PTY: true, Resize: true, Signals: []string{"SIGINT", "SIGTERM", "SIGKILL"}, Shells: []string{s.cfg.DefaultShell}, Transports: []string{"unix", "stdio"}}
}
func decode(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}
func errorCode(err error) int {
	switch {
	case errors.Is(err, executor.ErrNotFound), errors.Is(err, session.ErrNotFound):
		return protocol.ErrNotFound
	case errors.Is(err, executor.ErrConflict), errors.Is(err, session.ErrConflict):
		return protocol.ErrConflict
	case errors.Is(err, session.ErrBusy):
		return protocol.ErrBusy
	case errors.Is(err, executor.ErrCapacity), errors.Is(err, session.ErrCapacity):
		return protocol.ErrCapacity
	case errors.Is(err, policy.ErrRejected):
		return protocol.ErrPolicyRejected
	default:
		return protocol.InvalidParams
	}
}
