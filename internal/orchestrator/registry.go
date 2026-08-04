package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/agent-shell-tool/internal/protocol"
	"github.com/example/agent-shell-tool/pkg/client"
)

type toolState struct {
	spec         ToolSpec
	client       *client.Client
	capabilities json.RawMessage
	lastError    string
}
type Registry struct {
	toolsFile      string
	defaultTimeout time.Duration
	mu             sync.RWMutex
	tools          map[string]*toolState
	subscribers    map[uint64]chan ToolEvent
	nextSubscriber uint64
	closed         bool
}

func NewRegistry(toolsFile string, defaultTimeout time.Duration) (*Registry, error) {
	if defaultTimeout <= 0 {
		defaultTimeout = 2 * time.Minute
	}
	registry := &Registry{toolsFile: toolsFile, defaultTimeout: defaultTimeout, tools: make(map[string]*toolState), subscribers: make(map[uint64]chan ToolEvent)}
	if err := registry.Reload(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *Registry) Reload() error {
	cfg, err := LoadToolsFile(r.toolsFile)
	if err != nil {
		return err
	}
	states := make(map[string]*toolState, len(cfg.Tools))
	for _, spec := range cfg.Tools {
		states[spec.Name] = &toolState{spec: spec}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrToolUnavailable
	}
	previous := r.tools
	r.tools = states
	r.mu.Unlock()
	for _, state := range previous {
		if state.client != nil {
			_ = state.client.Close()
		}
	}
	return nil
}

func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	states := r.tools
	subscribers := r.subscribers
	r.subscribers = make(map[uint64]chan ToolEvent)
	r.mu.Unlock()
	for _, state := range states {
		if state.client != nil {
			_ = state.client.Close()
		}
	}
	for _, events := range subscribers {
		close(events)
	}
}

func (r *Registry) List(ctx context.Context, discover bool) []ToolInfo {
	r.mu.RLock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	result := make([]ToolInfo, 0, len(names))
	for _, name := range names {
		if discover {
			_, _ = r.ensureClient(ctx, name)
		}
		r.mu.RLock()
		state := r.tools[name]
		if state != nil {
			result = append(result, ToolInfo{ToolSpec: state.spec, Connected: state.client != nil, LastError: state.lastError, Capabilities: append(json.RawMessage(nil), state.capabilities...)})
		}
		r.mu.RUnlock()
	}
	return result
}

func (r *Registry) Resolve(method string) (string, error) {
	if method == "" || strings.HasPrefix(method, "system.") {
		return "", ErrMethodNotRoutable
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	match := ""
	for name, state := range r.tools {
		for _, prefix := range state.spec.MethodPrefixes {
			if strings.HasPrefix(method, prefix) {
				if match != "" {
					return "", ErrMethodNotRoutable
				}
				match = name
				break
			}
		}
	}
	if match == "" {
		return "", ErrMethodNotRoutable
	}
	return match, nil
}

func (r *Registry) Call(parent context.Context, params CallParams) (CallResult, error) {
	started := time.Now()
	if params.Method == "" {
		return CallResult{}, fmt.Errorf("%w: method is required", ErrInvalidRequest)
	}
	tool := params.Tool
	var err error
	if tool == "" {
		tool, err = r.Resolve(params.Method)
		if err != nil {
			return CallResult{}, err
		}
	}
	state, err := r.ensureClient(parent, tool)
	if err != nil {
		return CallResult{}, err
	}
	if !methodAllowed(state.spec, params.Method) {
		return CallResult{}, fmt.Errorf("%w: method %s is not allowed for %s", ErrInvalidRequest, params.Method, tool)
	}
	timeout := r.defaultTimeout
	if params.TimeoutMS < 0 {
		return CallResult{}, fmt.Errorf("%w: timeout_ms cannot be negative", ErrInvalidRequest)
	}
	if params.TimeoutMS > 0 {
		timeout = time.Duration(params.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	rawParams := params.Params
	if len(rawParams) == 0 {
		rawParams = []byte("{}")
	}
	var result json.RawMessage
	err = state.client.Call(ctx, params.Method, rawParams, &result)
	if err != nil {
		r.setError(tool, err)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return CallResult{}, err
		}
		var rpcErr *client.RPCError
		if errors.As(err, &rpcErr) {
			return CallResult{}, rpcErr
		}
		r.disconnect(tool, state)
		return CallResult{}, fmt.Errorf("%w: %s: %v", ErrToolUnavailable, tool, err)
	}
	r.clearError(tool)
	return CallResult{Tool: tool, Method: params.Method, Result: result, DurationMS: time.Since(started).Milliseconds()}, nil
}

func (r *Registry) Health(ctx context.Context) HealthResult {
	started := time.Now()
	r.mu.RLock()
	specs := make([]ToolSpec, 0, len(r.tools))
	for _, state := range r.tools {
		specs = append(specs, state.spec)
	}
	r.mu.RUnlock()
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	result := HealthResult{Status: "ok", Tools: make([]ToolHealth, len(specs))}
	var wg sync.WaitGroup
	for index, spec := range specs {
		wg.Add(1)
		go func(index int, spec ToolSpec) {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			params, _ := json.Marshal(map[string]any{})
			call, err := r.Call(callCtx, CallParams{Tool: spec.Name, Method: "system.health", Params: params, TimeoutMS: 2000})
			health := ToolHealth{Name: spec.Name, Required: spec.Required, Online: err == nil}
			if err != nil {
				health.Error = err.Error()
			} else {
				health.Health = call.Result
			}
			result.Tools[index] = health
		}(index, spec)
	}
	wg.Wait()
	for _, health := range result.Tools {
		if health.Required && !health.Online {
			result.Status = "degraded"
			break
		}
	}
	result.DurationMS = time.Since(started).Milliseconds()
	return result
}

func (r *Registry) Subscribe() (<-chan ToolEvent, func()) {
	r.mu.Lock()
	r.nextSubscriber++
	id := r.nextSubscriber
	events := make(chan ToolEvent, 256)
	r.subscribers[id] = events
	r.mu.Unlock()
	cancel := func() {
		r.mu.Lock()
		if current := r.subscribers[id]; current != nil {
			delete(r.subscribers, id)
			close(current)
		}
		r.mu.Unlock()
	}
	return events, cancel
}

func (r *Registry) ensureClient(ctx context.Context, name string) (*toolState, error) {
	r.mu.RLock()
	state := r.tools[name]
	if state != nil && state.client != nil {
		r.mu.RUnlock()
		return state, nil
	}
	r.mu.RUnlock()
	if state == nil {
		return nil, ErrToolNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state = r.tools[name]
	if state == nil {
		return nil, ErrToolNotFound
	}
	if state.client != nil {
		return state, nil
	}
	rpc, err := client.DialUnix(state.spec.Socket)
	if err != nil {
		state.lastError = err.Error()
		return nil, fmt.Errorf("%w: %s: %v", ErrToolUnavailable, name, err)
	}
	initCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var capabilities json.RawMessage
	err = rpc.Call(initCtx, "system.initialize", map[string]any{"protocol_version": protocol.Version, "client_info": map[string]any{"name": "agent-orchestrator"}}, &capabilities)
	if err != nil {
		_ = rpc.Close()
		state.lastError = err.Error()
		return nil, fmt.Errorf("%w: %s initialize: %v", ErrToolUnavailable, name, err)
	}
	state.client = rpc
	state.capabilities = capabilities
	state.lastError = ""
	go r.forwardNotifications(name, state, rpc)
	return state, nil
}

func (r *Registry) forwardNotifications(name string, state *toolState, rpc *client.Client) {
	for notification := range rpc.Notifications() {
		r.broadcast(ToolEvent{Tool: name, Method: notification.Method, Params: notification.Params, Timestamp: time.Now().UTC()})
	}
	r.disconnect(name, state)
}
func (r *Registry) broadcast(event ToolEvent) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, events := range r.subscribers {
		select {
		case events <- event:
		default:
		}
	}
}
func (r *Registry) disconnect(name string, expected *toolState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.tools[name]
	if state == expected && state.client != nil {
		_ = state.client.Close()
		state.client = nil
	}
}
func (r *Registry) setError(name string, err error) {
	r.mu.Lock()
	if state := r.tools[name]; state != nil {
		state.lastError = err.Error()
	}
	r.mu.Unlock()
}
func (r *Registry) clearError(name string) {
	r.mu.Lock()
	if state := r.tools[name]; state != nil {
		state.lastError = ""
	}
	r.mu.Unlock()
}
func methodAllowed(spec ToolSpec, method string) bool {
	if strings.HasPrefix(method, "system.") {
		return true
	}
	if len(spec.MethodPrefixes) == 0 {
		return true
	}
	for _, prefix := range spec.MethodPrefixes {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	return false
}
