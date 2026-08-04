package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/example/agent-shell-tool/internal/orchestrator"
	"github.com/example/agent-shell-tool/pkg/client"
)

type options struct {
	socket      string
	tool        string
	method      string
	params      string
	paramsFile  string
	requests    int
	concurrency int
	warmup      int
	timeout     time.Duration
}

type report struct {
	Tool          string   `json:"tool,omitempty"`
	Method        string   `json:"method"`
	Requests      int      `json:"requests"`
	Concurrency   int      `json:"concurrency"`
	Successes     int      `json:"successes"`
	Errors        int      `json:"errors"`
	DurationMS    float64  `json:"duration_ms"`
	ThroughputRPS float64  `json:"throughput_rps"`
	MinMS         float64  `json:"min_ms"`
	MeanMS        float64  `json:"mean_ms"`
	P50MS         float64  `json:"p50_ms"`
	P95MS         float64  `json:"p95_ms"`
	P99MS         float64  `json:"p99_ms"`
	MaxMS         float64  `json:"max_ms"`
	ErrorSamples  []string `json:"error_samples,omitempty"`
}

func main() {
	var cfg options
	flag.StringVar(&cfg.socket, "socket", "/run/agent/orchestrator.sock", "orchestrator Unix socket")
	flag.StringVar(&cfg.tool, "tool", "", "registered tool name; empty enables automatic routing")
	flag.StringVar(&cfg.method, "method", "", "downstream JSON-RPC method")
	flag.StringVar(&cfg.params, "params", "{}", "JSON params; supports {{id}} and {{run}} placeholders")
	flag.StringVar(&cfg.paramsFile, "params-file", "", "read JSON params template from a file")
	flag.IntVar(&cfg.requests, "requests", 1000, "measured request count")
	flag.IntVar(&cfg.concurrency, "concurrency", 8, "concurrent workers")
	flag.IntVar(&cfg.warmup, "warmup", 20, "warmup request count")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-request timeout")
	flag.Parse()
	if cfg.paramsFile != "" {
		data, err := os.ReadFile(cfg.paramsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read params file: %v\n", err)
			os.Exit(2)
		}
		cfg.params = string(data)
	}
	if err := validate(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	rpc, err := client.DialUnix(cfg.socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect orchestrator: %v\n", err)
		os.Exit(1)
	}
	defer rpc.Close()
	result := runLoad(rpc, cfg)
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	if result.Errors > 0 {
		os.Exit(1)
	}
}

func validate(cfg options) error {
	if cfg.method == "" {
		return fmt.Errorf("-method is required")
	}
	if cfg.requests <= 0 {
		return fmt.Errorf("-requests must be positive")
	}
	if cfg.concurrency <= 0 || cfg.concurrency > 1000 {
		return fmt.Errorf("-concurrency must be between 1 and 1000")
	}
	if cfg.warmup < 0 {
		return fmt.Errorf("-warmup cannot be negative")
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("-timeout must be positive")
	}
	probe := renderParams(cfg.params, 0, "probe")
	if !json.Valid([]byte(probe)) {
		return fmt.Errorf("-params must be valid JSON after placeholder expansion")
	}
	return nil
}

func runLoad(rpc *client.Client, cfg options) report {
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	for index := 0; index < cfg.warmup; index++ {
		_ = call(rpc, cfg, -(index + 1), runID)
	}
	jobs := make(chan int)
	latencies := make(chan time.Duration, cfg.requests)
	var failures atomic.Int64
	var errorMu sync.Mutex
	errorCounts := make(map[string]int)
	started := time.Now()
	var workers sync.WaitGroup
	for worker := 0; worker < cfg.concurrency; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				callStarted := time.Now()
				err := call(rpc, cfg, index, runID)
				elapsed := time.Since(callStarted)
				if err != nil {
					failures.Add(1)
					errorMu.Lock()
					errorCounts[err.Error()]++
					errorMu.Unlock()
					continue
				}
				latencies <- elapsed
			}
		}()
	}
	for index := 0; index < cfg.requests; index++ {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	close(latencies)
	elapsed := time.Since(started)
	values := make([]time.Duration, 0, cfg.requests-int(failures.Load()))
	for latency := range latencies {
		values = append(values, latency)
	}
	return makeReport(cfg, values, int(failures.Load()), elapsed, errorCounts)
}

func call(rpc *client.Client, cfg options, index int, runID string) error {
	params := json.RawMessage(renderParams(cfg.params, index, runID))
	request := orchestrator.CallParams{Tool: cfg.tool, Method: cfg.method, Params: params, TimeoutMS: cfg.timeout.Milliseconds()}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout+time.Second)
	defer cancel()
	var result orchestrator.CallResult
	return rpc.Call(ctx, "orchestrator.call", request, &result)
}
func renderParams(value string, index int, runID string) string {
	value = strings.ReplaceAll(value, "{{id}}", fmt.Sprintf("%d", index))
	return strings.ReplaceAll(value, "{{run}}", runID)
}

func makeReport(cfg options, values []time.Duration, failures int, elapsed time.Duration, errorCounts map[string]int) report {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	result := report{Tool: cfg.tool, Method: cfg.method, Requests: cfg.requests, Concurrency: cfg.concurrency, Successes: len(values), Errors: failures, DurationMS: milliseconds(elapsed)}
	if elapsed > 0 {
		result.ThroughputRPS = float64(cfg.requests) / elapsed.Seconds()
	}
	if len(values) > 0 {
		var total time.Duration
		for _, value := range values {
			total += value
		}
		result.MinMS = milliseconds(values[0])
		result.MeanMS = milliseconds(total / time.Duration(len(values)))
		result.P50MS = milliseconds(percentile(values, 0.50))
		result.P95MS = milliseconds(percentile(values, 0.95))
		result.P99MS = milliseconds(percentile(values, 0.99))
		result.MaxMS = milliseconds(values[len(values)-1])
	}
	type pair struct {
		message string
		count   int
	}
	pairs := make([]pair, 0, len(errorCounts))
	for message, count := range errorCounts {
		pairs = append(pairs, pair{message: message, count: count})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].count > pairs[j].count })
	for index, item := range pairs {
		if index >= 5 {
			break
		}
		result.ErrorSamples = append(result.ErrorSamples, fmt.Sprintf("%dx %s", item.count, item.message))
	}
	return result
}
func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1)*quantile + 0.5)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }
