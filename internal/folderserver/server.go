package folderserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/folderconfig"
	"github.com/example/agent-shell-tool/internal/folderpolicy"
	"github.com/example/agent-shell-tool/internal/folderreader"
	"github.com/example/agent-shell-tool/internal/protocol"
)

type Server struct {
	cfg          folderconfig.Config
	logger       *slog.Logger
	manager      *folderreader.Manager
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
}

type Health struct {
	Status      string `json:"status"`
	UptimeMS    int64  `json:"uptime_ms"`
	ActiveReads int    `json:"active_reads"`
	Workspace   string `json:"workspace"`
}

func New(cfg folderconfig.Config, logger *slog.Logger, policy folderpolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := folderreader.NewManager(folderreader.Config{
		Workspace: cfg.Workspace, MaxDepth: cfg.MaxDepth, MaxScannedEntries: cfg.MaxScannedEntries,
		MaxResults: cfg.MaxResults, DefaultLimit: cfg.DefaultLimit, MaxHashBytes: cfg.MaxHashBytes,
		MaxConcurrent: cfg.MaxConcurrent, MaxBatchItems: cfg.MaxBatchItems, IgnoredNames: cfg.IgnoredNames,
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
	s.logger.Info("read folder tool client connected", "client", conn.Identity())
	defer s.logger.Info("read folder tool client disconnected", "client", conn.Identity())
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
	case "read_folder.stat":
		var params folderreader.StatParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Stat(ctx, params)
			respond(result, err)
		}
	case "read_folder.list":
		var params folderreader.ListParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.List(ctx, params)
			respond(result, err)
		}
	case "read_folder.tree":
		var params folderreader.TreeParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Tree(ctx, params)
			respond(result, err)
		}
	case "read_folder.summary":
		var params folderreader.SummaryParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Summary(ctx, params)
			respond(result, err)
		}
	case "read_folder.snapshot":
		var params folderreader.SnapshotParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Snapshot(ctx, params)
			respond(result, err)
		}
	case "read_folder.compare":
		var params folderreader.CompareParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Compare(ctx, params)
			respond(result, err)
		}
	case "read_folder.batch":
		var params folderreader.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "read_folder.cancel":
		var params folderreader.CancelParams
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
		Methods: map[string]bool{"stat": true, "list": true, "tree": true, "summary": true, "snapshot": true, "compare": true, "batch": true, "cancel": true},
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
	case errors.Is(err, folderreader.ErrOutsideWorkspace):
		return protocol.ErrFolderOutsideWorkspace
	case errors.Is(err, folderreader.ErrSymlink):
		return protocol.ErrFolderSymlink
	case errors.Is(err, folderreader.ErrTooLarge):
		return protocol.ErrFolderTooLarge
	case errors.Is(err, folderreader.ErrCapacity):
		return protocol.ErrFolderCapacity
	case errors.Is(err, folderreader.ErrDuplicateRead):
		return protocol.ErrFolderDuplicateID
	case errors.Is(err, folderreader.ErrReadNotFound):
		return protocol.ErrFolderNotActive
	case errors.Is(err, context.Canceled):
		return protocol.ErrFolderCanceled
	case errors.Is(err, folderreader.ErrNotFolder):
		return protocol.ErrFolderNotFolder
	case errors.Is(err, folderreader.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrFolderInvalidRequest
	}
}
