package textsearch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/example/agent-shell-tool/internal/textsearchpolicy"
)

type Manager struct {
	cfg      Config
	resolver *Resolver
	policy   textsearchpolicy.Policy
	sem      chan struct{}
	active   atomic.Int64
	mu       sync.Mutex
	searches map[string]context.CancelFunc
	closed   bool
}

func NewManager(cfg Config, policy textsearchpolicy.Policy) (*Manager, error) {
	applyDefaults(&cfg)
	resolver, err := NewResolver(cfg.Workspace)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		policy = textsearchpolicy.AllowAll{}
	}
	return &Manager{cfg: cfg, resolver: resolver, policy: policy, sem: make(chan struct{}, cfg.MaxConcurrent), searches: make(map[string]context.CancelFunc)}, nil
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

func (m *Manager) Batch(ctx context.Context, params BatchParams) (BatchResult, error) {
	if len(params.Searches) == 0 || len(params.Searches) > m.cfg.MaxBatchItems {
		return BatchResult{}, fmt.Errorf("%w: invalid searches count", ErrInvalidRequest)
	}
	result := BatchResult{Results: make([]BatchItem, 0, len(params.Searches))}
	for _, request := range params.Searches {
		switch request.Kind {
		case "search", "regex":
			if request.Search == nil {
				return BatchResult{}, fmt.Errorf("%w: search parameters are required", ErrInvalidRequest)
			}
			item := *request.Search
			item.Regex = request.Kind == "regex"
			found, err := m.Search(ctx, item)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: request.Kind, Search: &found})
		case "multi":
			if request.Multi == nil {
				return BatchResult{}, fmt.Errorf("%w: multi parameters are required", ErrInvalidRequest)
			}
			found, err := m.Multi(ctx, *request.Multi)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: request.Kind, Search: &found})
		case "files":
			if request.Search == nil {
				return BatchResult{}, fmt.Errorf("%w: search parameters are required", ErrInvalidRequest)
			}
			found, err := m.Files(ctx, *request.Search)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: request.Kind, Files: &found})
		case "count":
			if request.Search == nil {
				return BatchResult{}, fmt.Errorf("%w: search parameters are required", ErrInvalidRequest)
			}
			found, err := m.Count(ctx, *request.Search)
			if err != nil {
				return BatchResult{}, err
			}
			result.Results = append(result.Results, BatchItem{Kind: request.Kind, Count: &found})
		default:
			return BatchResult{}, fmt.Errorf("%w: unsupported search kind %q", ErrInvalidRequest, request.Kind)
		}
	}
	return result, nil
}

func (m *Manager) begin(parent context.Context, requestedID string) (string, context.Context, func(), error) {
	if len(requestedID) > 128 || strings.IndexByte(requestedID, 0) >= 0 {
		return "", nil, nil, fmt.Errorf("%w: search_id is invalid", ErrInvalidRequest)
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
			return "", nil, nil, err
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
		return "", fmt.Errorf("generate search ID: %w", err)
	}
	return "text-search-" + base64.RawURLEncoding.EncodeToString(buffer), nil
}

func applyDefaults(cfg *Config) {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 32
	}
	if cfg.MaxScannedFiles <= 0 {
		cfg.MaxScannedFiles = 100000
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 8 << 20
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 1000
	}
	if cfg.DefaultLimit <= 0 {
		cfg.DefaultLimit = 100
	}
	if cfg.MaxMatchesFile <= 0 {
		cfg.MaxMatchesFile = 100
	}
	if cfg.MaxContextLines <= 0 {
		cfg.MaxContextLines = 20
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.MaxBatchItems <= 0 {
		cfg.MaxBatchItems = 20
	}
}
