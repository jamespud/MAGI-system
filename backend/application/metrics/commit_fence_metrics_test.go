package metrics_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestCommitFenceFallbacksAreExported(t *testing.T) {
	reg := metrics.New()
	reg.IncCommitFenceFallback(metrics.CommitFenceStatus)
	reg.IncCommitFenceFallback(metrics.CommitFenceStatus)
	reg.IncCommitFenceFallback(metrics.CommitFenceTerminal)
	reg.IncCommitFenceFallback(metrics.CommitFenceOp("not-a-fixed-label"))

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()

	if !strings.Contains(out, `magi_commit_fence_fallback_total{op="status"} 2`) {
		t.Fatalf("status series missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, `magi_commit_fence_fallback_total{op="terminal"} 1`) {
		t.Fatalf("terminal series missing or wrong:\n%s", out)
	}
	if got := reg.CommitFenceFallbacks(metrics.CommitFenceStatus); got != 2 {
		t.Fatalf("CommitFenceFallbacks(status) = %d, want 2", got)
	}
}

func TestCommitFenceFallbackIsNilSafe(t *testing.T) {
	var reg *metrics.Registry
	reg.IncCommitFenceFallback(metrics.CommitFenceTerminal) // must not panic
	if got := reg.CommitFenceFallbacks(metrics.CommitFenceTerminal); got != 0 {
		t.Fatalf("nil registry returned %d, want 0", got)
	}
}
