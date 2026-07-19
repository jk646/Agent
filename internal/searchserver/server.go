package searchserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/filesearch"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/searchconfig"
	"github.com/example/agent-shell-tool/internal/searchpolicy"
)

type Server struct {
	cfg          searchconfig.Config
	logger       *slog.Logger
	manager      *filesearch.Manager
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
	ProtocolVersion string          `json:"protocol_version"`
	Workspace       string          `json:"workspace"`
	Methods         map[string]bool `json:"methods"`
	IndexMode       string          `json:"index_mode"`
}

type Health struct {
	Status         string `json:"status"`
	UptimeMS       int64  `json:"uptime_ms"`
	ActiveSearches int    `json:"active_searches"`
	Workspace      string `json:"workspace"`
}

func New(cfg searchconfig.Config, logger *slog.Logger, policy searchpolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := filesearch.NewManager(filesearch.Config{
		Workspace: cfg.Workspace, MaxFileBytes: cfg.MaxFileBytes, MaxResults: cfg.MaxResults,
		MaxScannedEntries: cfg.MaxScannedEntries, MaxDepth: cfg.MaxDepth,
		MaxConcurrent: cfg.MaxConcurrent, IgnoredNames: cfg.IgnoredNames,
	}, policy)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg: cfg, logger: logger, manager: manager, shutdown: make(chan struct{}),
		done: make(chan struct{}), startedAt: time.Now(),
	}, nil
}

func (s *Server) ShutdownRequested() <-chan struct{} { return s.shutdown }
func (s *Server) Done() <-chan struct{}              { return s.done }

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdown)
		s.manager.Shutdown()
		deadline := time.Now().Add(s.cfg.ShutdownGrace)
		for s.manager.Active() > 0 && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		close(s.done)
	})
}

func (s *Server) ServeConn(raw io.ReadWriteCloser) {
	conn := protocol.NewConn(raw, s.cfg.MaxMessageBytes)
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.logger.Info("file search tool client connected", "client", conn.Identity())
	defer s.logger.Info("file search tool client disconnected", "client", conn.Identity())
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
		_ = conn.Respond(protocol.NewError(request.ID, errorCode(err), err.Error(), nil))
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
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveSearches: s.manager.Active(), Workspace: s.manager.Workspace()}, nil)
	case "system.shutdown":
		respond(map[string]bool{"accepted": true}, nil)
		go s.Shutdown()
	case "search.stat":
		var params filesearch.StatParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Stat(ctx, params)
			respond(result, err)
		}
	case "search.find":
		var params filesearch.FindParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Find(ctx, params)
			respond(result, err)
		}
	case "search.content":
		var params filesearch.ContentParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Content(ctx, params)
			respond(result, err)
		}
	case "search.batch":
		var params filesearch.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "search.cancel":
		var params filesearch.CancelParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Cancel(params)
			respond(result, err)
		}
	default:
		if len(request.ID) > 0 {
			_ = conn.Respond(protocol.NewError(request.ID, protocol.MethodNotFound, "method not found", request.Method))
		}
	}
}

func (s *Server) capabilities() Capabilities {
	return Capabilities{
		ProtocolVersion: protocol.Version, Workspace: s.manager.Workspace(), IndexMode: "direct",
		Methods: map[string]bool{"stat": true, "find": true, "content": true, "batch": true, "cancel": true},
	}
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

func decodeOrRespond(raw json.RawMessage, target any, respond func(any, error)) bool {
	if err := decode(raw, target); err != nil {
		respond(nil, err)
		return false
	}
	return true
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, filesearch.ErrOutsideWorkspace):
		return protocol.ErrSearchOutsideWorkspace
	case errors.Is(err, filesearch.ErrSymlink):
		return protocol.ErrSearchSymlink
	case errors.Is(err, filesearch.ErrTooLarge):
		return protocol.ErrSearchTooLarge
	case errors.Is(err, filesearch.ErrCapacity):
		return protocol.ErrSearchCapacity
	case errors.Is(err, filesearch.ErrDuplicateSearch):
		return protocol.ErrSearchDuplicateID
	case errors.Is(err, filesearch.ErrSearchNotFound):
		return protocol.ErrSearchNotActive
	case errors.Is(err, context.Canceled):
		return protocol.ErrSearchCanceled
	case errors.Is(err, filesearch.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrSearchInvalidRequest
	}
}
