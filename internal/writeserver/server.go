package writeserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/filewriter"
	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/internal/writeconfig"
	"github.com/example/agent-shell-tool/internal/writepolicy"
)

type Server struct {
	cfg          writeconfig.Config
	logger       *slog.Logger
	manager      *filewriter.Manager
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
	Status       string `json:"status"`
	UptimeMS     int64  `json:"uptime_ms"`
	ActiveWrites int    `json:"active_writes"`
	Workspace    string `json:"workspace"`
}

func New(cfg writeconfig.Config, logger *slog.Logger, policy writepolicy.Policy) (*Server, error) {
	if logger == nil {
		logger = slog.Default()
	}
	manager, err := filewriter.NewManager(filewriter.Config{Workspace: cfg.Workspace, TempDir: cfg.TempDir, MaxFileBytes: cfg.MaxFileBytes, MaxBatchFiles: cfg.MaxBatchFiles, MaxBatchBytes: cfg.MaxBatchBytes, MaxRollbackBytes: cfg.MaxRollbackBytes, MaxConcurrent: cfg.MaxConcurrent, JournalTTL: cfg.JournalTTL}, policy)
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
	s.logger.Info("write file tool client connected", "client", conn.Identity())
	defer s.logger.Info("write file tool client disconnected", "client", conn.Identity())
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
		respond(Health{Status: "ok", UptimeMS: time.Since(s.startedAt).Milliseconds(), ActiveWrites: s.manager.Active(), Workspace: s.manager.Workspace()}, nil)
	case "system.shutdown":
		respond(map[string]bool{"accepted": true}, nil)
		go s.Shutdown()
	case "write_file.create", "write_file.overwrite", "write_file.append", "write_file.write_at":
		var params filewriter.Operation
		if !decodeOrRespond(request.Params, &params, respond) {
			return
		}
		var result filewriter.Result
		var err error
		switch request.Method {
		case "write_file.create":
			result, err = s.manager.Create(ctx, params)
		case "write_file.overwrite":
			result, err = s.manager.Overwrite(ctx, params)
		case "write_file.append":
			result, err = s.manager.Append(ctx, params)
		case "write_file.write_at":
			result, err = s.manager.WriteAt(ctx, params)
		}
		respond(result, err)
	case "write_file.batch":
		var params filewriter.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Batch(ctx, params)
			respond(result, err)
		}
	case "write_file.preview":
		var params filewriter.BatchParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Preview(ctx, params)
			respond(result, err)
		}
	case "write_file.rollback":
		var params filewriter.RollbackParams
		if decodeOrRespond(request.Params, &params, respond) {
			result, err := s.manager.Rollback(ctx, params)
			respond(result, err)
		}
	case "write_file.cancel":
		var params filewriter.CancelParams
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
	return Capabilities{ProtocolVersion: protocol.Version, Workspace: s.manager.Workspace(), Methods: map[string]bool{"create": true, "overwrite": true, "append": true, "write_at": true, "batch": true, "preview": true, "rollback": true, "cancel": true}, Encodings: []string{"utf-8", "base64-binary"}}
}
func decodeOrRespond(raw json.RawMessage, target any, respond func(any, error)) bool {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		respond(nil, fmt.Errorf("%w: invalid params: %v", filewriter.ErrInvalidRequest, err))
		return false
	}
	return true
}
func errorCode(err error) int {
	switch {
	case errors.Is(err, filewriter.ErrOutsideWorkspace):
		return protocol.ErrWriteOutsideWorkspace
	case errors.Is(err, filewriter.ErrSymlink):
		return protocol.ErrWriteSymlink
	case errors.Is(err, filewriter.ErrTooLarge):
		return protocol.ErrWriteTooLarge
	case errors.Is(err, filewriter.ErrStaleFile):
		return protocol.ErrWriteStale
	case errors.Is(err, filewriter.ErrAlreadyExists):
		return protocol.ErrWriteAlreadyExists
	case errors.Is(err, filewriter.ErrUnsupportedType):
		return protocol.ErrWriteUnsupportedType
	case errors.Is(err, filewriter.ErrCapacity):
		return protocol.ErrWriteCapacity
	case errors.Is(err, filewriter.ErrDuplicateWrite):
		return protocol.ErrWriteDuplicateID
	case errors.Is(err, filewriter.ErrWriteNotFound):
		return protocol.ErrWriteNotActive
	case errors.Is(err, filewriter.ErrRollbackConflict):
		return protocol.ErrWriteRollbackConflict
	case errors.Is(err, filewriter.ErrTransactionNotFound):
		return protocol.ErrWriteTransaction
	case errors.Is(err, context.Canceled):
		return protocol.ErrWriteCanceled
	case errors.Is(err, filewriter.ErrNotFound):
		return protocol.ErrNotFound
	default:
		return protocol.ErrWriteInvalidRequest
	}
}
