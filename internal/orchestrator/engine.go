package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type Engine struct {
	registry *Registry
	sem      chan struct{}
	active   atomic.Int64
}

func NewEngine(registry *Registry, maxConcurrent int) *Engine {
	if maxConcurrent <= 0 {
		maxConcurrent = 32
	}
	return &Engine{registry: registry, sem: make(chan struct{}, maxConcurrent)}
}
func (e *Engine) Registry() *Registry { return e.registry }
func (e *Engine) Active() int         { return int(e.active.Load()) }
func (e *Engine) Call(ctx context.Context, params CallParams) (CallResult, error) {
	release, err := e.acquire(ctx)
	if err != nil {
		return CallResult{}, err
	}
	defer release()
	return e.registry.Call(ctx, params)
}

func (e *Engine) Batch(ctx context.Context, params BatchParams) (BatchResult, error) {
	started := time.Now()
	if len(params.Calls) == 0 || len(params.Calls) > 100 {
		return BatchResult{}, fmt.Errorf("%w: calls must contain 1 to 100 items", ErrInvalidRequest)
	}
	result := BatchResult{Results: make([]BatchItem, len(params.Calls))}
	if !params.Parallel {
		for index, call := range params.Calls {
			item, err := e.Call(ctx, call)
			result.Results[index] = BatchItem{Index: index}
			if err != nil {
				result.Results[index].Error = err.Error()
				if params.FailFast {
					for skipped := index + 1; skipped < len(params.Calls); skipped++ {
						result.Results[skipped] = BatchItem{Index: skipped, Error: "skipped after fail_fast error"}
					}
					break
				}
			} else {
				result.Results[index].Result = &item
			}
		}
		result.DurationMS = time.Since(started).Milliseconds()
		return result, nil
	}
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for index, call := range params.Calls {
		wg.Add(1)
		go func(index int, call CallParams) {
			defer wg.Done()
			item, err := e.Call(batchCtx, call)
			result.Results[index] = BatchItem{Index: index}
			if err != nil {
				result.Results[index].Error = err.Error()
				if params.FailFast {
					cancel()
				}
			} else {
				result.Results[index].Result = &item
			}
		}(index, call)
	}
	wg.Wait()
	result.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

func (e *Engine) acquire(ctx context.Context) (func(), error) {
	select {
	case e.sem <- struct{}{}:
		e.active.Add(1)
		return func() { e.active.Add(-1); <-e.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrCapacity
	}
}
