package orchestratorserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/orchestrator"
	"github.com/example/agent-shell-tool/internal/orchestratorconfig"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/pkg/client"
)

type Server struct {
	cfg          orchestratorconfig.Config
	logger       *slog.Logger
	registry     *orchestrator.Registry
	engine       *orchestrator.Engine
	shutdown     chan struct{}
	done         chan struct{}
	shutdownOnce sync.Once
	startedAt    time.Time
}
type InitializeParams struct {
	ProtocolVersion string         `json:"protocol_version"`
	ClientInfo      map[string]any `json:"client_info,omitempty"`
}
type ListParams struct {
	Discover bool `json:"discover,omitempty"`
}
type RouteParams struct {
	Method string `json:"method"`
}
type Health struct {
	Status          string `json:"status"`
	UptimeMS        int64  `json:"uptime_ms"`
	ActiveCalls     int    `json:"active_calls"`
	RegisteredTools int    `json:"registered_tools"`
}

func New(cfg orchestratorconfig.Config, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	registry, err := orchestrator.NewRegistry(cfg.ToolsFile, cfg.DefaultTimeout)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, logger: logger, registry: registry, engine: orchestrator.NewEngine(registry, cfg.MaxConcurrent), shutdown: make(chan struct{}), done: make(chan struct{}), startedAt: time.Now()}, nil
}
func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }
func (s *Server) Done() <-chan struct{}              { return s.done }
func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdown)
		deadline := time.Now().Add(s.cfg.ShutdownGrace)
		for s.engine.Active() > 0 && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		s.registry.Close()
		close(s.done)
	})
}

func (s *Server) ServeConn(raw io.ReadWriteCloser) {
	conn := protocol.NewConn(raw, s.cfg.MaxMessageBytes)
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := s.registry.Subscribe()
	defer unsubscribe()
	go func() {
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if err := conn.Notify("orchestrator.tool_event", event); err != nil {
					return
				}
			case <-ctx.Done():
				return
			case <-conn.Closed():
				return
			}
		}
	}()
	s.logger.Info("orchestrator client connected", "client", conn.Identity())
	defer s.logger.Info("orchestrator client disconnected", "client", conn.Identity())
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
		go s.handle(ctx, conn, request)
	}
}

func (s *Server) handle(ctx context.Context, conn *protocol.Conn, request protocol.Request) {
	respond := func(result any, err error) {
		if len(request.ID) == 0 {
			return
		}
		if err == nil {
			_ = conn.Respond(protocol.NewResponse(request.ID, result))
			return
		}
		code, data := errorCode(err)
		_ = conn.Respond(protocol.NewError(request.ID, code, err.Error(), data))
	}
	switch request.Method {
	case "system.initialize":
		var params InitializeParams
		if !decode(request.Params, &params, respond) {
			return
		}
		if params.ProtocolVersion != "" && params.ProtocolVersion != protocol.Version {
			respond(nil, fmt.Errorf("unsupported protocol version %q", params.ProtocolVersion))
			return
		}
		respond(map[string]any{"protocol_version": protocol.Version, "methods": []string{"orchestrator.tools", "orchestrator.reload", "orchestrator.route", "orchestrator.call", "orchestrator.batch", "orchestrator.health"}, "dynamic_tools": true, "tool_events": true}, nil)
	case "system.capabilities":
		respond(map[string]any{"protocol_version": protocol.Version, "dynamic_tools": true, "tool_events": true}, nil)
	case "system.health":
		tools := s.registry.List(ctx, false)
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveCalls: s.engine.Active(), RegisteredTools: len(tools)}, nil)
	case "system.shutdown":
		respond(map[string]bool{"accepted": true}, nil)
		go s.Shutdown()
	case "orchestrator.tools":
		var params ListParams
		if decode(request.Params, &params, respond) {
			respond(s.registry.List(ctx, params.Discover), nil)
		}
	case "orchestrator.reload":
		respond(map[string]bool{"reloaded": true}, s.registry.Reload())
	case "orchestrator.route":
		var params RouteParams
		if decode(request.Params, &params, respond) {
			tool, err := s.registry.Resolve(params.Method)
			respond(map[string]string{"tool": tool}, err)
		}
	case "orchestrator.call":
		var params orchestrator.CallParams
		if decode(request.Params, &params, respond) {
			result, err := s.engine.Call(ctx, params)
			respond(result, err)
		}
	case "orchestrator.batch":
		var params orchestrator.BatchParams
		if decode(request.Params, &params, respond) {
			result, err := s.engine.Batch(ctx, params)
			respond(result, err)
		}
	case "orchestrator.health":
		respond(s.registry.Health(ctx), nil)
	default:
		if len(request.ID) > 0 {
			_ = conn.Respond(protocol.NewError(request.ID, protocol.MethodNotFound, "method not found", request.Method))
		}
	}
}
func decode(raw json.RawMessage, target any, respond func(any, error)) bool {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		respond(nil, fmt.Errorf("%w: invalid params: %v", orchestrator.ErrInvalidRequest, err))
		return false
	}
	return true
}
func errorCode(err error) (int, any) {
	var rpcErr *client.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code, json.RawMessage(rpcErr.Data)
	}
	switch {
	case errors.Is(err, orchestrator.ErrToolNotFound):
		return protocol.ErrOrchestratorToolNotFound, nil
	case errors.Is(err, orchestrator.ErrMethodNotRoutable):
		return protocol.ErrOrchestratorRoute, nil
	case errors.Is(err, orchestrator.ErrToolUnavailable):
		return protocol.ErrOrchestratorUnavailable, nil
	case errors.Is(err, orchestrator.ErrCapacity):
		return protocol.ErrOrchestratorCapacity, nil
	case errors.Is(err, context.Canceled):
		return protocol.ErrOrchestratorInvalid, "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return protocol.ErrOrchestratorUnavailable, "timeout"
	default:
		return protocol.ErrOrchestratorInvalid, nil
	}
}
