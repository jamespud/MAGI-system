package metrics_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestRegistry_CountersAndPrometheus(t *testing.T) {
	reg := metrics.New()
	reg.IncCasesCreated()
	reg.IncRequests()
	reg.RunStart()
	reg.RunFinish(true)
	reg.IncToolCall(false)
	reg.AddTokens(120)

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()
	for _, want := range []string{
		"magi_cases_created_total 1",
		"magi_requests_total 1",
		"magi_runs_active 0",
		"magi_runs_completed_total 1",
		"magi_runs_failed_total 0",
		"magi_tool_calls_total 1",
		"magi_tool_call_failures_total 1",
		"magi_tokens_total 120",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRegistry_MemoryRetrievalFailures(t *testing.T) {
	reg := metrics.New()
	reg.IncMemoryRetrievalFailure()
	reg.IncMemoryRetrievalFailure()
	if reg.MemoryRetrievalFailures.Load() != 2 {
		t.Fatalf("expected 2 retrieval failures, got %d", reg.MemoryRetrievalFailures.Load())
	}
	var buf bytes.Buffer
	reg.WritePrometheus(&buf)
	if !strings.Contains(buf.String(), "magi_memory_retrieval_failures_total 2") {
		t.Fatalf("prometheus output missing retrieval failure counter:\n%s", buf.String())
	}
}
