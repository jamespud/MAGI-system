package metrics

import (
	"fmt"
	"io"
	"sync/atomic"
)

// Registry exposes atomic counters for the harness's operational health.
// All methods are nil-safe so dependencies can pass an optional registry.
type Registry struct {
	CasesCreated     atomic.Int64
	RunsActive       atomic.Int64
	RunsCompleted    atomic.Int64
	RunsFailed       atomic.Int64
	ToolCalls        atomic.Int64
	ToolCallFailures atomic.Int64
	TokensTotal      atomic.Int64
	RequestsTotal    atomic.Int64
}

func New() *Registry { return &Registry{} }

func (r *Registry) IncCasesCreated() {
	if r != nil {
		r.CasesCreated.Add(1)
	}
}
func (r *Registry) IncRequests() {
	if r != nil {
		r.RequestsTotal.Add(1)
	}
}
func (r *Registry) RunStart() {
	if r != nil {
		r.RunsActive.Add(1)
	}
}
func (r *Registry) AddTokens(n int64) {
	if r != nil {
		r.TokensTotal.Add(n)
	}
}

func (r *Registry) RunFinish(ok bool) {
	if r == nil {
		return
	}
	r.RunsActive.Add(-1)
	if ok {
		r.RunsCompleted.Add(1)
	} else {
		r.RunsFailed.Add(1)
	}
}

func (r *Registry) IncToolCall(ok bool) {
	if r == nil {
		return
	}
	r.ToolCalls.Add(1)
	if !ok {
		r.ToolCallFailures.Add(1)
	}
}

// WritePrometheus renders the counters in Prometheus text exposition format.
func (r *Registry) WritePrometheus(w io.Writer) {
	if r == nil {
		return
	}
	fmt.Fprintf(w, "# TYPE magi_cases_created_total counter\nmagi_cases_created_total %d\n", r.CasesCreated.Load())
	fmt.Fprintf(w, "# TYPE magi_runs_active gauge\nmagi_runs_active %d\n", r.RunsActive.Load())
	fmt.Fprintf(w, "# TYPE magi_runs_completed_total counter\nmagi_runs_completed_total %d\n", r.RunsCompleted.Load())
	fmt.Fprintf(w, "# TYPE magi_runs_failed_total counter\nmagi_runs_failed_total %d\n", r.RunsFailed.Load())
	fmt.Fprintf(w, "# TYPE magi_tool_calls_total counter\nmagi_tool_calls_total %d\n", r.ToolCalls.Load())
	fmt.Fprintf(w, "# TYPE magi_tool_call_failures_total counter\nmagi_tool_call_failures_total %d\n", r.ToolCallFailures.Load())
	fmt.Fprintf(w, "# TYPE magi_tokens_total counter\nmagi_tokens_total %d\n", r.TokensTotal.Load())
	fmt.Fprintf(w, "# TYPE magi_requests_total counter\nmagi_requests_total %d\n", r.RequestsTotal.Load())
}
