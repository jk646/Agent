package folderreader

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

	"github.com/example/agent-shell-tool/internal/folderpolicy"
)

type Manager struct {
	cfg      Config
	resolver *Resolver
	policy   folderpolicy.Policy
	sem      chan struct{}
	active   atomic.Int64
	mu       sync.Mutex
	reads    map[string]context.CancelFunc
	closed   bool
}

func NewManager(cfg Config, policy folderpolicy.Policy) (*Manager, error) {
	applyDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = folderpolicy.AllowAll{}
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

func (m *Manager) resolveFolder(ctx context.Context, kind, path string) (ResolvedPath, os.FileInfo, error) {
	resolved, err := m.resolver.Resolve(path)
	if err != nil {
		return ResolvedPath{}, nil, err
	}
	if err := m.policy.Authorize(ctx, folderpolicy.Operation{Kind: kind, Path: resolved.Relative}); err != nil {
		return ResolvedPath{}, nil, err
	}
	info, err := os.Stat(resolved.Absolute)
	if err != nil {
		return ResolvedPath{}, nil, mapPathError(err)
	}
	if !info.IsDir() {
		return ResolvedPath{}, nil, ErrNotFolder
	}
	return resolved, info, nil
}

func (m *Manager) begin(parent context.Context, requestedID string) (string, context.Context, func(), error) {
	if len(requestedID) > 128 || strings.IndexByte(requestedID, 0) >= 0 {
		return "", nil, nil, fmt.Errorf("%w: read_id is invalid", ErrInvalidRequest)
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
			return "", nil, nil, fmt.Errorf("generate folder read ID: %w", err)
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
	return "folder-read-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 32
	}
	if cfg.MaxScannedEntries <= 0 {
		cfg.MaxScannedEntries = 100000
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 5000
	}
	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = 100
	}
	if cfg.MaxHashBytes <= 0 {
		cfg.MaxHashBytes = 64 << 20
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.MaxBatchItems <= 0 {
		cfg.MaxBatchItems = 20
	}
}

func mapPathError(err error) error {
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func parentPath(value string) string {
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(value)))
	if parent == "." {
		return ""
	}
	return parent
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"
