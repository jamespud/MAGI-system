package metrics

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// runDurationBounds are Prometheus histogram bucket upper bounds in ms.
var runDurationBounds = []int64{100, 500, 1000, 5000, 30000, 120000, 600000}

// a2aDurationBounds are the fixed histogram buckets (ms) for bounded A2A
// request/stream durations and event lag.
var a2aDurationBounds = []int64{10, 50, 100, 250, 1000, 5000, 30000}

const lenA2ADurationBounds = 7

// Registry exposes operational counters, a run-duration histogram and cost
// accounting. All methods are nil-safe so dependencies can pass an optional
// registry.
type Registry struct {
	CasesCreated      atomic.Int64
	RunsActive        atomic.Int64
	RunsCompleted     atomic.Int64
	RunsFailed        atomic.Int64
	ToolCalls         atomic.Int64
	ToolCallFailures  atomic.Int64
	ToolOutputClipped atomic.Int64
	TokensTotal       atomic.Int64
	RequestsTotal     atomic.Int64

	MemoryRetrievalFailures atomic.Int64
	ModelFailovers          atomic.Int64
	WebSearchFailovers      atomic.Int64
	FeedbackViolations      atomic.Int64
	BenchmarkAutoRuns       atomic.Int64
	BenchmarkRegressionFail atomic.Int64

	artifactPersistFailures [lenArtifactKinds]atomic.Int64

	RunDurationSumMs   atomic.Int64
	RunDurationCount   atomic.Int64
	RunDurationBuckets [8]atomic.Int64 // 7 bounded buckets + +Inf
	CostTotalMicro     atomic.Int64    // USD * 1e6

	// A2A exposes a bounded-label surface for the A2A server.
	A2AActiveStreams     atomic.Int64
	A2AIdempotencyHits   atomic.Int64
	a2aRequests          [lenA2AOperations][lenA2AResults]atomic.Int64
	a2aProjectionErr     [lenA2AProjectionKinds]atomic.Int64
	A2ARequestDuration   a2aHistogram
	A2AStreamDuration    a2aHistogram
	A2AEventLag          a2aHistogram
	A2ARecoveryAttempted atomic.Int64
	A2ARecoverySettled   atomic.Int64
	A2ARecoveryRetried   atomic.Int64

	perUser sync.Map // userID -> *userUsage
}

// a2aHistogram is a fixed-bucket histogram in milliseconds. The final slot is
// +Inf.
type a2aHistogram struct {
	sum     atomic.Int64
	count   atomic.Int64
	buckets [lenA2ADurationBounds + 1]atomic.Int64
}

func (h *a2aHistogram) record(ms int64) {
	if ms < 0 {
		ms = 0
	}
	h.sum.Add(ms)
	h.count.Add(1)
	for i, bound := range a2aDurationBounds {
		if ms <= bound {
			h.buckets[i].Add(1)
		}
	}
	h.buckets[len(a2aDurationBounds)].Add(1)
}

func (h *a2aHistogram) writePrometheus(w io.Writer, name string) {
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	fmt.Fprintf(w, "%s_sum %d\n", name, h.sum.Load())
	fmt.Fprintf(w, "%s_count %d\n", name, h.count.Load())
	for i, bound := range a2aDurationBounds {
		fmt.Fprintf(w, "%s_bucket{le=\"%d\"} %d\n", name, bound, h.buckets[i].Load())
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", name, h.buckets[len(a2aDurationBounds)].Load())
}

// A2AOperation is a fixed label value for A2A request metrics. User input can
// never create new values: callers must pick one of the constants.
type A2AOperation string

const (
	A2AOperationGetTask         A2AOperation = "GetTask"
	A2AOperationListTasks       A2AOperation = "ListTasks"
	A2AOperationCancelTask      A2AOperation = "CancelTask"
	A2AOperationSendMessage     A2AOperation = "SendMessage"
	A2AOperationSendStreaming   A2AOperation = "SendStreamingMessage"
	A2AOperationSubscribeToTask A2AOperation = "SubscribeToTask"
)

const lenA2AOperations = 6

var a2aOperations = [...]A2AOperation{
	A2AOperationGetTask, A2AOperationListTasks, A2AOperationCancelTask,
	A2AOperationSendMessage, A2AOperationSendStreaming, A2AOperationSubscribeToTask,
}

// A2AResult is a fixed label value for the outcome of an A2A request.
type A2AResult string

const (
	A2AResultOK    A2AResult = "ok"
	A2AResultError A2AResult = "error"
)

const lenA2AResults = 2

var a2aResults = [...]A2AResult{A2AResultOK, A2AResultError}

// A2AProjectionKind is a fixed label value for projection failures.
type A2AProjectionKind string

const (
	A2AProjectionArtifact A2AProjectionKind = "artifact"
	A2AProjectionStatus   A2AProjectionKind = "status"
)

const lenA2AProjectionKinds = 2

var a2aProjectionKinds = [...]A2AProjectionKind{A2AProjectionArtifact, A2AProjectionStatus}

// ArtifactKind is a fixed label value for durable artifact writes. The kinds
// mirror the writes in orchestration.persistArtifacts plus the two best-effort
// side writes, so a dropped audit row is visible instead of silent.
type ArtifactKind string

const (
	ArtifactAgentRun         ArtifactKind = "agent_run"
	ArtifactEvidence         ArtifactKind = "evidence"
	ArtifactClaim            ArtifactKind = "claim"
	ArtifactToolCall         ArtifactKind = "tool_call"
	ArtifactVote             ArtifactKind = "vote"
	ArtifactDebateRound      ArtifactKind = "debate_round"
	ArtifactReflection       ArtifactKind = "reflection"
	ArtifactTaskSnapshot     ArtifactKind = "task_snapshot"
	ArtifactMemoryProjection ArtifactKind = "memory_projection"
)

const lenArtifactKinds = 9

var artifactKinds = [...]ArtifactKind{
	ArtifactAgentRun, ArtifactEvidence, ArtifactClaim, ArtifactToolCall, ArtifactVote,
	ArtifactDebateRound, ArtifactReflection, ArtifactTaskSnapshot, ArtifactMemoryProjection,
}

// IncArtifactPersistFailure records a durable write that failed and was
// tolerated. The kind must be one of the fixed constants above.
func (r *Registry) IncArtifactPersistFailure(kind ArtifactKind) {
	if r == nil {
		return
	}
	for i, k := range artifactKinds {
		if k == kind {
			r.artifactPersistFailures[i].Add(1)
			return
		}
	}
}

// ArtifactPersistFailures returns the current count for a fixed kind.
func (r *Registry) ArtifactPersistFailures(kind ArtifactKind) int64 {
	if r == nil {
		return 0
	}
	for i, k := range artifactKinds {
		if k == kind {
			return r.artifactPersistFailures[i].Load()
		}
	}
	return 0
}

// IncA2ARequest records one A2A protocol request by its fixed operation and
// result labels.
func (r *Registry) IncA2ARequest(op A2AOperation, res A2AResult) {
	if r == nil {
		return
	}
	oi := indexOfA2AOperation(op)
	ri := indexOfA2AResult(res)
	if oi < 0 || ri < 0 {
		return
	}
	r.a2aRequests[oi][ri].Add(1)
}

// A2AStreamStart increments the active-stream gauge.
func (r *Registry) A2AStreamStart() {
	if r != nil {
		r.A2AActiveStreams.Add(1)
	}
}

// A2AStreamEnd decrements the active-stream gauge.
func (r *Registry) A2AStreamEnd() {
	if r != nil {
		r.A2AActiveStreams.Add(-1)
	}
}

// IncA2AIdempotencyHit records a replayed equal-hash submission.
func (r *Registry) IncA2AIdempotencyHit() {
	if r != nil {
		r.A2AIdempotencyHits.Add(1)
	}
}

// IncA2AProjectionError records a deterministic projection failure by kind.
func (r *Registry) IncA2AProjectionError(kind A2AProjectionKind) {
	if r == nil {
		return
	}
	ki := indexOfA2AProjectionKind(kind)
	if ki < 0 {
		return
	}
	r.a2aProjectionErr[ki].Add(1)
}

// RecordA2ARequestDuration records one bounded A2A request duration in ms.
func (r *Registry) RecordA2ARequestDuration(ms int64) {
	if r != nil {
		r.A2ARequestDuration.record(ms)
	}
}

// RecordA2AStreamDuration records one bounded A2A stream lifetime in ms.
func (r *Registry) RecordA2AStreamDuration(ms int64) {
	if r != nil {
		r.A2AStreamDuration.record(ms)
	}
}

// RecordA2AEventLag records how long a durable event waited before the stream
// applied it, in ms.
func (r *Registry) RecordA2AEventLag(ms int64) {
	if r != nil {
		r.A2AEventLag.record(ms)
	}
}

// IncA2ARecoveryAttempted records one claim attempt by the recovery sweep.
func (r *Registry) IncA2ARecoveryAttempted() {
	if r != nil {
		r.A2ARecoveryAttempted.Add(1)
	}
}

// IncA2ARecoverySettled records one recovery attempt that settled durably.
func (r *Registry) IncA2ARecoverySettled() {
	if r != nil {
		r.A2ARecoverySettled.Add(1)
	}
}

// IncA2ARecoveryRetried records one recovery sweep that found claimable work.
func (r *Registry) IncA2ARecoveryRetried() {
	if r != nil {
		r.A2ARecoveryRetried.Add(1)
	}
}

func indexOfA2AOperation(op A2AOperation) int {
	for i, v := range a2aOperations {
		if v == op {
			return i
		}
	}
	return -1
}

func indexOfA2AResult(res A2AResult) int {
	for i, v := range a2aResults {
		if v == res {
			return i
		}
	}
	return -1
}

func indexOfA2AProjectionKind(kind A2AProjectionKind) int {
	for i, v := range a2aProjectionKinds {
		if v == kind {
			return i
		}
	}
	return -1
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

// IncModelFailover records one automatic move from a failed model provider to
// the next configured provider.
func (r *Registry) IncModelFailover() {
	if r != nil {
		r.ModelFailovers.Add(1)
	}
}

// IncWebSearchFailover records one automatic move from a failed search
// provider to the next configured provider.
func (r *Registry) IncWebSearchFailover() {
	if r != nil {
		r.WebSearchFailovers.Add(1)
	}
}

// AddFeedbackViolations records deterministic check violations returned by
// the check_output feedback sensor.
func (r *Registry) AddFeedbackViolations(n int64) {
	if r != nil && n > 0 {
		r.FeedbackViolations.Add(n)
	}
}

// IncBenchmarkAutoRun records one automatically triggered benchmark run.
func (r *Registry) IncBenchmarkAutoRun() {
	if r != nil {
		r.BenchmarkAutoRuns.Add(1)
	}
}

// IncBenchmarkRegressionFailure records an automated regression gate failure.
func (r *Registry) IncBenchmarkRegressionFailure() {
	if r != nil {
		r.BenchmarkRegressionFail.Add(1)
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

// IncToolOutputClipped counts tool results that were shortened before being
// persisted, so a provider returning oversized payloads is visible in metrics
// instead of only showing up as a missing audit row.
func (r *Registry) IncToolOutputClipped() {
	if r == nil {
		return
	}
	r.ToolOutputClipped.Add(1)
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
	fmt.Fprintf(w, "# TYPE magi_tool_output_clipped_total counter\nmagi_tool_output_clipped_total %d\n", r.ToolOutputClipped.Load())
	fmt.Fprintln(w, "# TYPE magi_artifact_persist_failures_total counter")
	for i, kind := range artifactKinds {
		fmt.Fprintf(w, "magi_artifact_persist_failures_total{kind=%q} %d\n", kind, r.artifactPersistFailures[i].Load())
	}
	fmt.Fprintf(w, "# TYPE magi_tokens_total counter\nmagi_tokens_total %d\n", r.TokensTotal.Load())
	fmt.Fprintf(w, "# TYPE magi_requests_total counter\nmagi_requests_total %d\n", r.RequestsTotal.Load())
	fmt.Fprintf(w, "# TYPE magi_memory_retrieval_failures_total counter\nmagi_memory_retrieval_failures_total %d\n", r.MemoryRetrievalFailures.Load())
	fmt.Fprintf(w, "# TYPE magi_model_failovers_total counter\nmagi_model_failovers_total %d\n", r.ModelFailovers.Load())
	fmt.Fprintf(w, "# TYPE magi_web_search_failovers_total counter\nmagi_web_search_failovers_total %d\n", r.WebSearchFailovers.Load())
	fmt.Fprintf(w, "# TYPE magi_feedback_violations_total counter\nmagi_feedback_violations_total %d\n", r.FeedbackViolations.Load())
	fmt.Fprintf(w, "# TYPE magi_benchmark_auto_runs_total counter\nmagi_benchmark_auto_runs_total %d\n", r.BenchmarkAutoRuns.Load())
	fmt.Fprintf(w, "# TYPE magi_benchmark_regression_failures_total counter\nmagi_benchmark_regression_failures_total %d\n", r.BenchmarkRegressionFail.Load())

	fmt.Fprintf(w, "# TYPE magi_run_duration_ms histogram\n")
	fmt.Fprintf(w, "magi_run_duration_ms_sum %d\n", r.RunDurationSumMs.Load())
	fmt.Fprintf(w, "magi_run_duration_ms_count %d\n", r.RunDurationCount.Load())
	for i, b := range runDurationBounds {
		fmt.Fprintf(w, "magi_run_duration_ms_bucket{le=\"%d\"} %d\n", b, r.RunDurationBuckets[i].Load())
	}
	fmt.Fprintf(w, "magi_run_duration_ms_bucket{le=\"+Inf\"} %d\n", r.RunDurationBuckets[len(runDurationBounds)].Load())

	fmt.Fprintf(w, "# TYPE magi_cost_usd_total counter\nmagi_cost_usd_total %.6f\n", float64(r.CostTotalMicro.Load())/1e6)

	// Prometheus allows at most one TYPE declaration per metric family, so
	// emit it once before the fixed operation x result series.
	fmt.Fprintln(w, "# TYPE magi_a2a_requests_total counter")
	for oi, op := range a2aOperations {
		for ri, res := range a2aResults {
			// Always expose the fixed series (zero-value) so canaries and SLO
			// dashboards can rely on every operation x result label existing.
			fmt.Fprintf(w, "magi_a2a_requests_total{operation=%q,result=%q} %d\n", op, res, r.a2aRequests[oi][ri].Load())
		}
	}
	if n := r.A2AActiveStreams.Load(); n != 0 {
		fmt.Fprintf(w, "# TYPE magi_a2a_active_streams gauge\nmagi_a2a_active_streams %d\n", n)
	}
	if n := r.A2AIdempotencyHits.Load(); n > 0 {
		fmt.Fprintf(w, "# TYPE magi_a2a_idempotency_hits_total counter\nmagi_a2a_idempotency_hits_total %d\n", n)
	}
	for ki, kind := range a2aProjectionKinds {
		if n := r.a2aProjectionErr[ki].Load(); n > 0 {
			fmt.Fprintf(w, "# TYPE magi_a2a_projection_errors_total counter\nmagi_a2a_projection_errors_total{kind=%q} %d\n", kind, n)
		}
	}
	r.A2ARequestDuration.writePrometheus(w, "magi_a2a_request_duration_ms")
	r.A2AStreamDuration.writePrometheus(w, "magi_a2a_stream_duration_ms")
	r.A2AEventLag.writePrometheus(w, "magi_a2a_event_lag_ms")
	fmt.Fprintf(w, "# TYPE magi_a2a_recovery_attempted_total counter\nmagi_a2a_recovery_attempted_total %d\n", r.A2ARecoveryAttempted.Load())
	fmt.Fprintf(w, "# TYPE magi_a2a_recovery_settled_total counter\nmagi_a2a_recovery_settled_total %d\n", r.A2ARecoverySettled.Load())
	fmt.Fprintf(w, "# TYPE magi_a2a_recovery_retried_total counter\nmagi_a2a_recovery_retried_total %d\n", r.A2ARecoveryRetried.Load())
}
