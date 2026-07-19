package filesearch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/example/agent-shell-tool/internal/searchpolicy"
)

type Manager struct {
	cfg      Config
	resolver *Resolver
	policy   searchpolicy.Policy
	sem      chan struct{}
	active   atomic.Int64
	mu       sync.Mutex
	searches map[string]context.CancelFunc
	closed   bool
}

func NewManager(cfg Config, policy searchpolicy.Policy) (*Manager, error) {
	applyDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = searchpolicy.AllowAll{}
	}
	return &Manager{
		cfg: cfg, resolver: resolver, policy: policy,
		sem: make(chan struct{}, cfg.MaxConcurrent), searches: make(map[string]context.CancelFunc),
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
	cancels := make([]context.CancelFunc, 0, len(m.searches))
	for _, cancel := range m.searches {
		cancels = append(cancels, cancel)
	}
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

func (m *Manager) Cancel(params CancelParams) (CancelResult, error) {
	if params.SearchID == "" {
		return CancelResult{}, fmt.Errorf("%w: search_id is required", ErrInvalidRequest)
	}
	m.mu.Lock()
	cancel := m.searches[params.SearchID]
	m.mu.Unlock()
	if cancel == nil {
		return CancelResult{}, ErrSearchNotFound
	}
	cancel()
	return CancelResult{SearchID: params.SearchID, Canceled: true}, nil
}

func (m *Manager) Stat(ctx context.Context, params StatParams) (Entry, error) {
	resolved, err := m.resolver.Resolve(params.Path)
	if err != nil {
		return Entry{}, err
	}
	if err := m.policy.Authorize(ctx, searchpolicy.Operation{Kind: "stat", Path: resolved.Relative}); err != nil {
		return Entry{}, err
	}
	info, err := os.Lstat(resolved.Absolute)
	if err != nil {
		return Entry{}, mapPathError(err)
	}
	return makeEntry(resolved.Relative, info, 0), nil
}

func (m *Manager) Batch(ctx context.Context, params BatchParams) (BatchResult, error) {
	if len(params.Searches) == 0 || len(params.Searches) > 20 {
		return BatchResult{}, fmt.Errorf("%w: searches must contain 1 to 20 items", ErrInvalidRequest)
	}
	result := BatchResult{Results: make([]BatchItem, 0, len(params.Searches))}
	for _, request := range params.Searches {
		switch request.Kind {
		case "find":
			if request.Find == nil {
				return BatchResult{}, fmt.Errorf("%w: find parameters are required", ErrInvalidRequest)
			}
			found, err := m.Find(ctx, *request.Find)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "find", Find: &found})
		case "content":
			if request.Content == nil {
				return BatchResult{}, fmt.Errorf("%w: content parameters are required", ErrInvalidRequest)
			}
			found, err := m.Content(ctx, *request.Content)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: "content", Content: &found})
		default:
			return BatchResult{}, fmt.Errorf("%w: unsupported search kind %q", ErrInvalidRequest, request.Kind)
		}
	}
	return result, nil
}

func (m *Manager) begin(parent context.Context, requestedID string) (string, context.Context, func(), error) {
	if len(requestedID) > 128 {
		return "", nil, nil, fmt.Errorf("%w: search_id is too long", ErrInvalidRequest)
	}
	select {
	case m.sem <- struct{}{}:
	default:
		return "", nil, nil, ErrCapacity
	}
	searchID := requestedID
	if searchID == "" {
		var err error
		searchID, err = newSearchID()
		if err != nil {
			<-m.sem
			return "", nil, nil, fmt.Errorf("generate search ID: %w", err)
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
	if _, exists := m.searches[searchID]; exists {
		m.mu.Unlock()
		cancel()
		<-m.sem
		return "", nil, nil, ErrDuplicateSearch
	}
	m.searches[searchID] = cancel
	m.active.Add(1)
	m.mu.Unlock()
	release := func() {
		cancel()
		m.mu.Lock()
		delete(m.searches, searchID)
		m.active.Add(-1)
		m.mu.Unlock()
		<-m.sem
	}
	return searchID, ctx, release, nil
}

func newSearchID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "search-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 8 << 20
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 1000
	}
	if cfg.MaxScannedEntries <= 0 {
		cfg.MaxScannedEntries = 200000
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 64
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
}

func makeEntry(relative string, info os.FileInfo, score int) Entry {
	return Entry{
		Path: filepath.ToSlash(relative), Name: info.Name(), Type: entryType(info), Size: info.Size(),
		Mode: fmt.Sprintf("%04o", info.Mode().Perm()), ModifiedAt: info.ModTime().UTC().Format(timeFormat), Score: score,
	}
}

func entryType(info os.FileInfo) string {
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
