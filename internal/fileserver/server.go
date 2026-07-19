package fileserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/fileconfig"
	"github.com/example/agent-shell-tool/internal/filepolicy"
	"github.com/example/agent-shell-tool/internal/filetool"
	"github.com/example/agent-shell-tool/internal/protocol"
)

type Server struct {
	cfg          fileconfig.Config
	logger       *slog.Logger
	manager      *filetool.Manager
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
	Status             string `json:"status"`
	UptimeMS           int64  `json:"uptime_ms"`
	ActiveTransactions int    `json:"active_transactions"`
	Workspace          string `json:"workspace"`
}

func New(cfg fileconfig.Config, logger *slog.Logger, policy filepolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := filetool.NewManager(filetool.Config{
		Workspace:           cfg.Workspace,
		TempDir:             cfg.TempDir,
		MaxFileBytes:        cfg.MaxFileBytes,
		MaxReadBytes:        cfg.MaxReadBytes,
		MaxEntries:          cfg.MaxEntries,
		MaxSearchMatches:    cfg.MaxSearchMatches,
		MaxTransactionFiles: cfg.MaxTransactionFiles,
		MaxTransactionBytes: cfg.MaxTransactionBytes,
		MaxDiffBytes:        cfg.MaxDiffBytes,
		MaxConcurrent:       cfg.MaxConcurrent,
		JournalTTL:          cfg.JournalTTL,
	}, policy)
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
		deadline := time.Now().Add(s.cfg.ShutdownGrace)
		for s.manager.Active() > 0 && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
		}
		s.manager.Shutdown()
		close(s.done)
	})
}

func (s *Server) ServeConn(raw io.ReadWriteCloser) {
	conn := protocol.NewConn(raw, s.cfg.MaxMessageBytes)
	defer conn.Close()
	s.logger.Info("file tool client connected", "client", conn.Identity())
	defer s.logger.Info("file tool client disconnected", "client", conn.Identity())
	ctx := context.Background()
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
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveTransactions: s.manager.Active(), Workspace: s.manager.Workspace()}, nil)
	case "system.shutdown":
		respond(map[string]bool{"accepted": true}, nil)
		go s.Shutdown()
	case "file.stat":
		var params filetool.StatParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Stat(ctx, params)
			respond(result, err)
		}
	case "file.read":
		var params filetool.ReadParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Read(ctx, params)
			respond(result, err)
		}
	case "file.list":
		var params filetool.ListParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.List(ctx, params)
			respond(result, err)
		}
	case "file.find":
		var params filetool.FindParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Find(ctx, params)
			respond(result, err)
		}
	case "file.search":
		var params filetool.SearchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Search(ctx, params)
			respond(result, err)
		}
	case "file.apply_edits":
		var params filetool.ApplyEditsParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.ApplyEdits(ctx, params)
			respond(result, err)
		}
	case "file.batch":
		var params filetool.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "file.rollback", "file.restore":
		var params filetool.RollbackParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Rollback(ctx, params)
			respond(result, err)
		}
	case "file.create", "file.mkdir", "file.copy", "file.move", "file.delete", "file.chmod":
		var operation filetool.Operation
		if decodeOrRespond(request.Params, &operation, respond) {
			operation.Kind = request.Method[len("file."):]
			result, err := s.manager.Batch(ctx, filetool.BatchParams{Operations: []filetool.Operation{operation}})
			respond(result, err)
		}
	default:
		if len(request.ID) > 0 {
			_ = conn.Respond(protocol.NewError(request.ID, protocol.MethodNotFound, "method not found", request.Method))
		}
	}
}

func (s *Server) capabilities() Capabilities {
	return Capabilities{ProtocolVersion: protocol.Version, Workspace: s.manager.Workspace(), Methods: map[string]bool{
		"stat": true, "read": true, "list": true, "find": true, "search": true,
		"create": true, "mkdir": true, "copy": true, "move": true, "delete": true,
		"chmod": true, "apply_edits": true, "batch": true, "rollback": true,
	}}
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
	case errors.Is(err, filetool.ErrOutsideWorkspace):
		return protocol.ErrFileOutsideWorkspace
	case errors.Is(err, filetool.ErrStaleFile):
		return protocol.ErrFileStale
	case errors.Is(err, filetool.ErrMatchNotFound):
		return protocol.ErrFileMatchNotFound
	case errors.Is(err, filetool.ErrMatchCount):
		return protocol.ErrFileMatchCount
	case errors.Is(err, filetool.ErrTooLarge), errors.Is(err, filetool.ErrCapacity):
		return protocol.ErrFileTooLarge
	case errors.Is(err, filetool.ErrBinaryFile):
		return protocol.ErrFileBinary
	case errors.Is(err, filetool.ErrAlreadyExists):
		return protocol.ErrFileAlreadyExists
	case errors.Is(err, filetool.ErrSymlink):
		return protocol.ErrFileSymlink
	case errors.Is(err, filetool.ErrRollbackConflict):
		return protocol.ErrFileRollbackConflict
	case errors.Is(err, filetool.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrFileInvalidOperation
	}
}
