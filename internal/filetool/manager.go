package filetool

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/agent-shell-tool/internal/filepolicy"
)

type Manager struct {
	cfg       Config
	resolver  *Resolver
	policy    filepolicy.Policy
	locks     *lockManager
	journal   *journalManager
	sem       chan struct{}
	active    atomic.Int64
	closed    chan struct{}
	closeOnce sync.Once
}

func NewManager(cfg Config, policy filepolicy.Policy) (*Manager, error) {
	applyConfigDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = filepolicy.AllowAll{}
	}
	if err := os.MkdirAll(cfg.TempDir, 0o700); err != nil {
		return nil, fmt.Errorf("create file tool temp directory: %w", err)
	}
	manager := &Manager{
		cfg:      cfg,
		resolver: resolver,
		policy:   policy,
		locks:    newLockManager(),
		sem:      make(chan struct{}, cfg.MaxConcurrent),
		closed:   make(chan struct{}),
	}
	manager.journal = newJournalManager(cfg.TempDir, cfg.JournalTTL, cfg.MaxTransactionBytes, cfg.MaxTransactionFiles)
	go manager.reapLoop()
	return manager, nil
}

func (m *Manager) Workspace() string { return m.resolver.Root() }
func (m *Manager) Active() int       { return int(m.active.Load()) }

func (m *Manager) Shutdown() {
	m.closeOnce.Do(func() { close(m.closed) })
}

func (m *Manager) authorize(ctx context.Context, operation Operation) error {
	return m.policy.Authorize(ctx, filepolicy.Operation{Kind: operation.Kind, Path: operation.Path, From: operation.From, To: operation.To})
}

func (m *Manager) acquireSlot() (func(), error) {
	select {
	case m.sem <- struct{}{}:
		m.active.Add(1)
		return func() { m.active.Add(-1); <-m.sem }, nil
	default:
		return nil, ErrCapacity
	}
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

func applyConfigDefaults(cfg *Config) {
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 8 << 20
	}
	if cfg.MaxReadBytes <= 0 {
		cfg.MaxReadBytes = 256 << 10
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 1000
	}
	if cfg.MaxSearchMatches <= 0 {
		cfg.MaxSearchMatches = 500
	}
	if cfg.MaxTransactionFiles <= 0 {
		cfg.MaxTransactionFiles = 100
	}
	if cfg.MaxTransactionBytes <= 0 {
		cfg.MaxTransactionBytes = 64 << 20
	}
	if cfg.MaxDiffBytes <= 0 {
		cfg.MaxDiffBytes = 1 << 20
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.JournalTTL <= 0 {
		cfg.JournalTTL = 15 * time.Minute
	}
}
