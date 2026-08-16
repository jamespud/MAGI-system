package metrics

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// runDurationBounds are Prometheus histogram bucket upper bounds in ms.
var runDurationBounds = []int64{100, 500, 1000, 5000, 30000, 120000, 600000}

// Registry exposes operational counters, a run-duration histogram and cost
// accounting. All methods are nil-safe so dependencies can pass an optional
// registry.
type Registry struct {
	CasesCreated     atomic.Int64
	RunsActive       atomic.Int64
	RunsCompleted    atomic.Int64
	RunsFailed       atomic.Int64
	ToolCalls        atomic.Int64
	ToolCallFailures atomic.Int64
	TokensTotal      atomic.Int64
	RequestsTotal    atomic.Int64

	MemoryRetrievalFailures atomic.Int64

	RunDurationSumMs   atomic.Int64
	RunDurationCount   atomic.Int64
	RunDurationBuckets [8]atomic.Int64 // 7 bounded buckets + +Inf
	CostTotalMicro     atomic.Int64    // USD * 1e6

	perUser sync.Map // userID -> *userUsage
}

type userUsage struct {
	tokens     atomic.Int64
	costMicro  atomic.Int64
	runsActive atomic.Int64
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

// IncMemoryRetrievalFailure records a failed long-term-memory retrieval. RAG
// failures are surfaced here (plus logs/events) instead of silently degrading
// the agent context (P0: D3).
func (r *Registry) IncMemoryRetrievalFailure() {
	if r != nil {
		r.MemoryRetrievalFailures.Add(1)
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

// AddTokensForUser records token usage against a specific user (per-user
// status display). Falls back to the global counter when userID is empty.
func (r *Registry) AddTokensForUser(userID string, n int64) {
	if r == nil {
		return
	}
	r.AddTokens(n)
	if userID == "" {
		return
	}
	u := r.userUsage(userID)
	u.tokens.Add(n)
}

// AddCostUSDForUser records cost against a specific user.
func (r *Registry) AddCostUSDForUser(userID string, usd float64) {
	if r == nil {
		return
	}
	r.AddCostUSD(usd)
	if userID == "" {
		return
	}
	r.userUsage(userID).costMicro.Add(int64(usd * 1e6))
}

// RunStartForUser marks a run active for a user.
func (r *Registry) RunStartForUser(userID string) {
	if r == nil || userID == "" {
		return
	}
	r.userUsage(userID).runsActive.Add(1)
}

// RunFinishForUser marks a run inactive for a user.
func (r *Registry) RunFinishForUser(userID string) {
	if r == nil || userID == "" {
		return
	}
	r.userUsage(userID).runsActive.Add(-1)
}

// UserStatus returns the per-user usage snapshot for the status endpoint.
func (r *Registry) UserStatus(userID string) (tokens int64, costUSD float64, runsActive int64) {
	if r == nil || userID == "" {
		return r.TokensTotal.Load(), float64(r.CostTotalMicro.Load()) / 1e6, r.RunsActive.Load()
	}
	u := r.userUsage(userID)
	return u.tokens.Load(), float64(u.costMicro.Load()) / 1e6, u.runsActive.Load()
}

func (r *Registry) userUsage(userID string) *userUsage {
	v, _ := r.perUser.LoadOrStore(userID, &userUsage{})
	return v.(*userUsage)
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

// RecordRunDuration records one run duration in milliseconds.
func (r *Registry) RecordRunDuration(ms int64) {
	if r == nil {
		return
	}
	r.RunDurationSumMs.Add(ms)
	r.RunDurationCount.Add(1)
	for i, b := range runDurationBounds {
		if ms <= b {
			r.RunDurationBuckets[i].Add(1)
		}
	}
	r.RunDurationBuckets[len(runDurationBounds)].Add(1)
}

// AddCostUSD records an estimated model cost in USD.
func (r *Registry) AddCostUSD(usd float64) {
	if r != nil {
		r.CostTotalMicro.Add(int64(usd * 1e6))
	}
}

// WritePrometheus renders the counters and histogram in Prometheus text format.
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
	fmt.Fprintf(w, "# TYPE magi_memory_retrieval_failures_total counter\nmagi_memory_retrieval_failures_total %d\n", r.MemoryRetrievalFailures.Load())

	fmt.Fprintf(w, "# TYPE magi_run_duration_ms histogram\n")
	fmt.Fprintf(w, "magi_run_duration_ms_sum %d\n", r.RunDurationSumMs.Load())
	fmt.Fprintf(w, "magi_run_duration_ms_count %d\n", r.RunDurationCount.Load())
	for i, b := range runDurationBounds {
		fmt.Fprintf(w, "magi_run_duration_ms_bucket{le=\"%d\"} %d\n", b, r.RunDurationBuckets[i].Load())
	}
	fmt.Fprintf(w, "magi_run_duration_ms_bucket{le=\"+Inf\"} %d\n", r.RunDurationBuckets[len(runDurationBounds)].Load())

	fmt.Fprintf(w, "# TYPE magi_cost_usd_total counter\nmagi_cost_usd_total %.6f\n", float64(r.CostTotalMicro.Load())/1e6)
}
