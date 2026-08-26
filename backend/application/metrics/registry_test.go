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
	reg.IncModelFailover()
	reg.IncWebSearchFailover()
	reg.AddFeedbackViolations(3)
	reg.IncBenchmarkAutoRun()
	reg.IncBenchmarkRegressionFailure()
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
		"magi_model_failovers_total 1",
		"magi_web_search_failovers_total 1",
		"magi_feedback_violations_total 3",
		"magi_benchmark_auto_runs_total 1",
		"magi_benchmark_regression_failures_total 1",
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

func TestRegistry_A2AMetricsWithBoundedLabels(t *testing.T) {
	reg := metrics.New()
	reg.IncA2ARequest(metrics.A2AOperationGetTask, metrics.A2AResultOK)
	reg.IncA2ARequest(metrics.A2AOperationGetTask, metrics.A2AResultError)
	reg.A2AStreamStart()
	reg.IncA2AIdempotencyHit()
	reg.IncA2AProjectionError(metrics.A2AProjectionArtifact)

	var buf strings.Builder
	reg.WritePrometheus(&buf)
	out := buf.String()
	for _, want := range []string{
		`magi_a2a_requests_total{operation="GetTask",result="ok"} 1`,
		`magi_a2a_requests_total{operation="GetTask",result="error"} 1`,
		"magi_a2a_active_streams 1",
		"magi_a2a_idempotency_hits_total 1",
		`magi_a2a_projection_errors_total{kind="artifact"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRegistry_A2AMetricsIgnoreUnknownLabels(t *testing.T) {
	reg := metrics.New()
	reg.IncA2ARequest("user-injected-operation", metrics.A2AResultOK)
	reg.IncA2ARequest(metrics.A2AOperationGetTask, "user-injected-result")
	reg.IncA2AProjectionError("user-injected-kind")
	var buf strings.Builder
	reg.WritePrometheus(&buf)
	if strings.Contains(buf.String(), "user-injected") {
		t.Fatalf("unknown labels leaked into metrics output:\n%s", buf.String())
	}
}

func TestRegistry_A2ADurationsRecoveryAndZeroValueSeries(t *testing.T) {
	reg := metrics.New()
	reg.RecordA2ARequestDuration(25)
	reg.RecordA2ARequestDuration(3000)
	reg.RecordA2AStreamDuration(120)
	reg.RecordA2AEventLag(8)
	reg.IncA2ARecoveryAttempted()
	reg.IncA2ARecoverySettled()
	reg.IncA2ARecoveryRetried()

	var buf strings.Builder
	reg.WritePrometheus(&buf)
	out := buf.String()
	for _, want := range []string{
		"magi_a2a_request_duration_ms_count 2",
		"magi_a2a_request_duration_ms_sum 3025",
		`magi_a2a_request_duration_ms_bucket{le="50"} 1`,
		`magi_a2a_request_duration_ms_bucket{le="5000"} 2`,
		"magi_a2a_stream_duration_ms_count 1",
		"magi_a2a_event_lag_ms_count 1",
		"magi_a2a_recovery_attempted_total 1",
		"magi_a2a_recovery_settled_total 1",
		"magi_a2a_recovery_retried_total 1",
		// Every fixed operation x result series is always exposed (zero-value).
		`magi_a2a_requests_total{operation="SendMessage",result="ok"} 0`,
		`magi_a2a_requests_total{operation="CancelTask",result="error"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
