package main

import (
	"testing"
	"time"
)

func TestRenderParams(t *testing.T) {
	result := renderParams(`{"path":"perf/{{run}}/{{id}}.txt"}`, 42, "abc")
	if result != `{"path":"perf/abc/42.txt"}` {
		t.Fatalf("unexpected params %s", result)
	}
}

func TestWarmupIDsDoNotOverlapMeasuredIDs(t *testing.T) {
	warmup := renderParams(`{"id":"{{id}}"}`, -1, "run")
	measured := renderParams(`{"id":"{{id}}"}`, 0, "run")
	if warmup == measured {
		t.Fatal("warmup and measured IDs must differ")
	}
}
func TestPercentile(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}
	if got := percentile(values, 0.50); got != 3*time.Millisecond {
		t.Fatalf("unexpected p50 %s", got)
	}
	if got := percentile(values, 0.95); got != 5*time.Millisecond {
		t.Fatalf("unexpected p95 %s", got)
	}
}
func TestValidate(t *testing.T) {
	valid := options{method: "system.health", params: `{}`, requests: 1, concurrency: 1, timeout: time.Second}
	if err := validate(valid); err != nil {
		t.Fatal(err)
	}
	valid.params = `{"bad":`
	if err := validate(valid); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
