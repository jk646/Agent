package textsearchserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/textsearch"
	"github.com/example/agent-shell-tool/internal/textsearchconfig"
	"github.com/example/agent-shell-tool/internal/textsearchpolicy"
)

type Server struct {
	cfg          textsearchconfig.Config
	logger       *slog.Logger
	manager      *textsearch.Manager
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
	Encodings       []string        `json:"encodings"`
}
type Health struct {
	Status         string `json:"status"`
	UptimeMS       int64  `json:"uptime_ms"`
	ActiveSearches int    `json:"active_searches"`
	Workspace      string `json:"workspace"`
}

func New(cfg textsearchconfig.Config, logger *slog.Logger, policy textsearchpolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := textsearch.NewManager(textsearch.Config{Workspace: cfg.Workspace, MaxDepth: cfg.MaxDepth, MaxScannedFiles: cfg.MaxScannedFiles, MaxFileBytes: cfg.MaxFileBytes, MaxResults: cfg.MaxResults, DefaultLimit: cfg.DefaultLimit, MaxMatchesFile: cfg.MaxMatchesFile, MaxContextLines: cfg.MaxContextLines, MaxConcurrent: cfg.MaxConcurrent, MaxBatchItems: cfg.MaxBatchItems, IgnoredNames: cfg.IgnoredNames}, policy)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, logger: logger, manager: manager, shutdown: make(chan struct{}), done: make(chan struct{}), startedAt: time.Now()}, nil
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
	s.logger.Info("search text tool client connected", "client", conn.Identity())
	defer s.logger.Info("search text tool client disconnected", "client", conn.Identity())
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
		if !decodeOrRespond(request.Params, &params, respond) {
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
	case "search_text.search", "search_text.regex", "search_text.files", "search_text.count":
		var params textsearch.SearchParams
		if !decodeOrRespond(request.Params, &params, respond) {
			return
		}
		if request.Method == "search_text.search" {
			params.Regex = false
		}
		if request.Method == "search_text.regex" {
			params.Regex = true
		}
		switch request.Method {
		case "search_text.files":
			result, err := s.manager.Files(ctx, params)
			respond(result, err)
		case "search_text.count":
			result, err := s.manager.Count(ctx, params)
			respond(result, err)
		default:
			result, err := s.manager.Search(ctx, params)
			respond(result, err)
		}
	case "search_text.multi":
		var params textsearch.MultiParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Multi(ctx, params)
			respond(result, err)
		}
	case "search_text.batch":
		var params textsearch.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "search_text.cancel":
		var params textsearch.CancelParams
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
	return Capabilities{ProtocolVersion: protocol.Version, Workspace: s.manager.Workspace(), Methods: map[string]bool{"search": true, "regex": true, "multi": true, "files": true, "count": true, "batch": true, "cancel": true}, Encodings: []string{"utf-8", "utf-8-bom", "utf-16le", "utf-16be"}}
}

func decodeOrRespond(raw json.RawMessage, target any, respond func(any, error)) bool {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		respond(nil, fmt.Errorf("%w: invalid params: %v", textsearch.ErrInvalidRequest, err))
		return false
	}
	return true
}

func errorCode(err error) int {
	switch {
	case errors.Is(err, textsearch.ErrOutsideWorkspace):
		return protocol.ErrTextSearchOutsideWorkspace
	case errors.Is(err, textsearch.ErrSymlink):
		return protocol.ErrTextSearchSymlink
	case errors.Is(err, textsearch.ErrLimitExceeded):
		return protocol.ErrTextSearchLimit
	case errors.Is(err, textsearch.ErrUnsupportedEncoding):
		return protocol.ErrTextSearchEncoding
	case errors.Is(err, textsearch.ErrCapacity):
		return protocol.ErrTextSearchCapacity
	case errors.Is(err, textsearch.ErrDuplicateSearch):
		return protocol.ErrTextSearchDuplicateID
	case errors.Is(err, textsearch.ErrSearchNotFound):
		return protocol.ErrTextSearchNotActive
	case errors.Is(err, context.Canceled):
		return protocol.ErrTextSearchCanceled
	case errors.Is(err, textsearch.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrTextSearchInvalidRequest
	}
}
