package filereader

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/example/agent-shell-tool/internal/readpolicy"
)

type Manager struct {
	cfg      Config
	resolver *Resolver
	policy   readpolicy.Policy
	sem      chan struct{}
	active   atomic.Int64
	mu       sync.Mutex
	reads    map[string]context.CancelFunc
	closed   bool
}

func NewManager(cfg Config, policy readpolicy.Policy) (*Manager, error) {
	applyDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = readpolicy.AllowAll{}
	}
	return &Manager{
		cfg: cfg, resolver: resolver, policy: policy,
		sem: make(chan struct{}, cfg.MaxConcurrent), reads: make(map[string]context.CancelFunc),
	}, nil
}

func (m *Manager) Workspace() string { return m.resolver.Root() }
func (m *Manager) Active() int       { return int(m.active.Load()) }

func (m *Manager) Shutdown() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	cancels := make([]context.CancelFunc, 0, len(m.reads))
	for _, cancel := range m.reads {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Manager) Cancel(params CancelParams) (CancelResult, error) {
	if params.ReadID == "" {
		return CancelResult{}, fmt.Errorf("%w: read_id is required", ErrInvalidRequest)
	}
	m.mu.Lock()
	cancel := m.reads[params.ReadID]
	m.mu.Unlock()
	if cancel == nil {
		return CancelResult{}, ErrReadNotFound
	}
	cancel()
	return CancelResult{ReadID: params.ReadID, Canceled: true}, nil
}

func (m *Manager) Stat(ctx context.Context, params StatParams) (FileInfo, error) {
	resolved, err := m.resolveAndAuthorize(ctx, "stat", params.Path)
	if err != nil {
		return FileInfo{}, err
	}
	info, err := os.Lstat(resolved.Absolute)
	if err != nil {
		return FileInfo{}, mapPathError(err)
	}
	result := fileInfo(resolved.Relative, info)
	if info.Mode().IsRegular() {
		result.Encoding = detectEncodingFromFile(resolved.Absolute)
	}
	if params.IncludeHash {
		if !info.Mode().IsRegular() {
			return FileInfo{}, ErrNotRegular
		}
		result.SHA256, err = hashFile(ctx, resolved.Absolute, info, m.cfg.MaxHashBytes)
	}
	return result, err
}

func (m *Manager) Batch(ctx context.Context, params BatchParams) (BatchResult, error) {
	if len(params.Reads) == 0 || len(params.Reads) > m.cfg.MaxBatchItems {
		return BatchResult{}, fmt.Errorf("%w: reads must contain 1 to %d items", ErrInvalidRequest, m.cfg.MaxBatchItems)
	}
	result := BatchResult{Results: make([]BatchItem, 0, len(params.Reads))}
	for _, request := range params.Reads {
		switch request.Kind {
		case "stat":
			if request.Stat == nil {
				return BatchResult{}, fmt.Errorf("%w: stat parameters are required", ErrInvalidRequest)
			}
			item, err := m.Stat(ctx, *request.Stat)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "stat", Stat: &item})
		case "text":
			if request.Text == nil {
				return BatchResult{}, fmt.Errorf("%w: text parameters are required", ErrInvalidRequest)
			}
			item, err := m.Text(ctx, *request.Text)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "text", Text: &item})
		case "lines":
			if request.Lines == nil {
				return BatchResult{}, fmt.Errorf("%w: lines parameters are required", ErrInvalidRequest)
			}
			item, err := m.Lines(ctx, *request.Lines)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "lines", Lines: &item})
		case "bytes", "binary":
			if request.Bytes == nil {
				return BatchResult{}, fmt.Errorf("%w: bytes parameters are required", ErrInvalidRequest)
			}
			item, err := m.Bytes(ctx, *request.Bytes)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: request.Kind, Bytes: &item})
		case "hash":
			if request.Hash == nil {
				return BatchResult{}, fmt.Errorf("%w: hash parameters are required", ErrInvalidRequest)
			}
			item, err := m.Hash(ctx, *request.Hash)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "hash", Hash: &item})
		default:
			return BatchResult{}, fmt.Errorf("%w: unsupported read kind %q", ErrInvalidRequest, request.Kind)
		}
	}
	return result, nil
}

func (m *Manager) resolveAndAuthorize(ctx context.Context, kind, path string) (ResolvedPath, error) {
	resolved, err := m.resolver.Resolve(path)
	if err != nil {
		return ResolvedPath{}, err
	}
	if err := m.policy.Authorize(ctx, readpolicy.Operation{Kind: kind, Path: resolved.Relative}); err != nil {
		return ResolvedPath{}, err
	}
	return resolved, nil
}

func (m *Manager) begin(parent context.Context, requestedID string) (string, context.Context, func(), error) {
	if len(requestedID) > 128 || strings.IndexByte(requestedID, 0) >= 0 {
		return "", nil, nil, fmt.Errorf("%w: read_id is too long", ErrInvalidRequest)
	}
	select {
	case m.sem <- struct{}{}:
	default:
		return "", nil, nil, ErrCapacity
	}
	readID := requestedID
	if readID == "" {
		var err error
		readID, err = newReadID()
		if err != nil {
			<-m.sem
			return "", nil, nil, fmt.Errorf("generate read ID: %w", err)
		}
	}
	ctx, cancel := context.WithCancel(parent)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		<-m.sem
		return "", nil, nil, ErrCapacity
	}
	if _, exists := m.reads[readID]; exists {
		m.mu.Unlock()
		cancel()
		<-m.sem
		return "", nil, nil, ErrDuplicateRead
	}
	m.reads[readID] = cancel
	m.active.Add(1)
	m.mu.Unlock()
	release := func() {
		cancel()
		m.mu.Lock()
		delete(m.reads, readID)
		m.active.Add(-1)
		m.mu.Unlock()
		<-m.sem
	}
	return readID, ctx, release, nil
}

func newReadID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "read-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxTextBytes <= 0 {
		cfg.MaxTextBytes = 8 << 20
	}
	if cfg.MaxChunkBytes <= 0 {
		cfg.MaxChunkBytes = 1 << 20
	}
	if cfg.MaxHashBytes <= 0 {
		cfg.MaxHashBytes = 1 << 30
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.MaxBatchItems <= 0 {
		cfg.MaxBatchItems = 20
	}
}

func fileInfo(relative string, info os.FileInfo) FileInfo {
	return FileInfo{
		Path: filepath.ToSlash(relative), Type: fileType(info), Size: info.Size(),
		Mode: fmt.Sprintf("%04o", info.Mode().Perm()), ModifiedAt: info.ModTime().UTC().Format(timeFormat),
	}
}

func fileType(info os.FileInfo) string {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	case info.IsDir():
		return "directory"
	case info.Mode().IsRegular():
		return "file"
	default:
		return "other"
	}
}

func mapPathError(err error) error {
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"
