package readserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/filereader"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/readconfig"
	"github.com/example/agent-shell-tool/internal/readpolicy"
)

type Server struct {
	cfg          readconfig.Config
	logger       *slog.Logger
	manager      *filereader.Manager
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
	Status      string `json:"status"`
	UptimeMS    int64  `json:"uptime_ms"`
	ActiveReads int    `json:"active_reads"`
	Workspace   string `json:"workspace"`
}

func New(cfg readconfig.Config, logger *slog.Logger, policy readpolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := filereader.NewManager(filereader.Config{
		Workspace: cfg.Workspace, MaxTextBytes: cfg.MaxTextBytes, MaxChunkBytes: cfg.MaxChunkBytes,
		MaxHashBytes: cfg.MaxHashBytes, MaxConcurrent: cfg.MaxConcurrent, MaxBatchItems: cfg.MaxBatchItems,
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
	s.logger.Info("read file tool client connected", "client", conn.Identity())
	defer s.logger.Info("read file tool client disconnected", "client", conn.Identity())
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
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveReads: s.manager.Active(), Workspace: s.manager.Workspace()}, nil)
	case "system.shutdown":
		respond(map[string]bool{"accepted": true}, nil)
		go s.Shutdown()
	case "read.stat":
		var params filereader.StatParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Stat(ctx, params)
			respond(result, err)
		}
	case "read.text":
		var params filereader.TextParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Text(ctx, params)
			respond(result, err)
		}
	case "read.lines":
		var params filereader.LinesParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Lines(ctx, params)
			respond(result, err)
		}
	case "read.bytes", "read.binary":
		var params filereader.BytesParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Bytes(ctx, params)
			respond(result, err)
		}
	case "read.hash":
		var params filereader.HashParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Hash(ctx, params)
			respond(result, err)
		}
	case "read.batch":
		var params filereader.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "read.cancel":
		var params filereader.CancelParams
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
		ProtocolVersion: protocol.Version, Workspace: s.manager.Workspace(),
		Methods:   map[string]bool{"stat": true, "text": true, "lines": true, "bytes": true, "binary": true, "hash": true, "batch": true, "cancel": true},
		Encodings: []string{"utf-8", "utf-8-bom", "utf-16le", "utf-16be"},
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
	case errors.Is(err, filereader.ErrOutsideWorkspace):
		return protocol.ErrReadOutsideWorkspace
	case errors.Is(err, filereader.ErrSymlink):
		return protocol.ErrReadSymlink
	case errors.Is(err, filereader.ErrTooLarge):
		return protocol.ErrReadTooLarge
	case errors.Is(err, filereader.ErrUnsupportedEncoding):
		return protocol.ErrReadUnsupportedEncoding
	case errors.Is(err, filereader.ErrCapacity):
		return protocol.ErrReadCapacity
	case errors.Is(err, filereader.ErrDuplicateRead):
		return protocol.ErrReadDuplicateID
	case errors.Is(err, filereader.ErrReadNotFound):
		return protocol.ErrReadNotActive
	case errors.Is(err, context.Canceled):
		return protocol.ErrReadCanceled
	case errors.Is(err, filereader.ErrFileChanged):
		return protocol.ErrReadFileChanged
	case errors.Is(err, filereader.ErrNotRegular):
		return protocol.ErrReadNotRegular
	case errors.Is(err, filereader.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrReadInvalidRequest
	}
}
