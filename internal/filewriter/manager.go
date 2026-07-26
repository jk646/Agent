package filewriter

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/agent-shell-tool/internal/writepolicy"
)

type Manager struct {
	cfg       Config
	resolver  *Resolver
	policy    writepolicy.Policy
	locks     *lockManager
	journal   *journalManager
	sem       chan struct{}
	active    atomic.Int64
	mu        sync.Mutex
	writes    map[string]context.CancelFunc
	closed    chan struct{}
	closeOnce sync.Once
}

func NewManager(cfg Config, policy writepolicy.Policy) (*Manager, error) {
	applyDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = writepolicy.AllowAll{}
	}
	if err := os.MkdirAll(cfg.TempDir, 0o700); err != nil {
		return nil, err
	}
	manager := &Manager{cfg: cfg, resolver: resolver, policy: policy, locks: newLockManager(), sem: make(chan struct{}, cfg.MaxConcurrent), writes: make(map[string]context.CancelFunc), closed: make(chan struct{})}
	manager.journal = newJournalManager(cfg.TempDir, cfg.JournalTTL, cfg.MaxRollbackBytes)
	go manager.reapLoop()
	return manager, nil
}

func (m *Manager) Workspace() string { return m.resolver.Root() }
func (m *Manager) Active() int       { return int(m.active.Load()) }
func (m *Manager) Shutdown() {
	m.closeOnce.Do(func() {
		close(m.closed)
		m.mu.Lock()
		for _, cancel := range m.writes {
			cancel()
		}
		m.mu.Unlock()
	})
}

func (m *Manager) Cancel(params CancelParams) (CancelResult, error) {
	if params.WriteID == "" {
		return CancelResult{}, fmt.Errorf("%w: write_id is required", ErrInvalidRequest)
	}
	m.mu.Lock()
	cancel := m.writes[params.WriteID]
	m.mu.Unlock()
	if cancel == nil {
		return CancelResult{}, ErrWriteNotFound
	}
	cancel()
	return CancelResult{WriteID: params.WriteID, Canceled: true}, nil
}

func (m *Manager) begin(parent context.Context, requested string) (string, context.Context, func(), error) {
	if len(requested) > 128 || strings.IndexByte(requested, 0) >= 0 {
		return "", nil, nil, ErrInvalidRequest
	}
	select {
	case m.sem <- struct{}{}:
	default:
		return "", nil, nil, ErrCapacity
	}
	id := requested
	if id == "" {
		var err error
		id, err = randomID("write-")
		if err != nil {
			<-m.sem
			return "", nil, nil, err
		}
	}
	ctx, cancel := context.WithCancel(parent)
	m.mu.Lock()
	select {
	case <-m.closed:
		m.mu.Unlock()
		cancel()
		<-m.sem
		return "", nil, nil, ErrCapacity
	default:
	}
	if _, exists := m.writes[id]; exists {
		m.mu.Unlock()
		cancel()
		<-m.sem
		return "", nil, nil, ErrDuplicateWrite
	}
	m.writes[id] = cancel
	m.active.Add(1)
	m.mu.Unlock()
	release := func() {
		cancel()
		m.mu.Lock()
		delete(m.writes, id)
		m.active.Add(-1)
		m.mu.Unlock()
		<-m.sem
	}
	return id, ctx, release, nil
}

func (m *Manager) reapLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.journal.reap()
		case <-m.closed:
			return
		}
	}
}
func applyDefaults(cfg *Config) {
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 8 << 20
	}
	if cfg.MaxBatchFiles <= 0 {
		cfg.MaxBatchFiles = 100
	}
	if cfg.MaxBatchBytes <= 0 {
		cfg.MaxBatchBytes = 64 << 20
	}
	if cfg.MaxRollbackBytes <= 0 {
		cfg.MaxRollbackBytes = 256 << 20
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.JournalTTL <= 0 {
		cfg.JournalTTL = 15 * time.Minute
	}
}
func randomID(prefix string) (string, error) {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(data), nil
}
