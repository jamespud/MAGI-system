package metrics_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestArtifactPersistFailuresAreExported(t *testing.T) {
	reg := metrics.New()
	reg.IncArtifactPersistFailure(metrics.ArtifactEvidence)
	reg.IncArtifactPersistFailure(metrics.ArtifactEvidence)
	reg.IncArtifactPersistFailure(metrics.ArtifactToolCall)

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()

	if !strings.Contains(out, `magi_artifact_persist_failures_total{kind="evidence"} 2`) {
		t.Fatalf("evidence series missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, `magi_artifact_persist_failures_total{kind="tool_call"} 1`) {
		t.Fatalf("tool_call series missing or wrong:\n%s", out)
	}
	// Every fixed kind is exposed even at zero, so dashboards can rely on it.
	if !strings.Contains(out, `magi_artifact_persist_failures_total{kind="vote"} 0`) {
		t.Fatalf("zero-value series missing:\n%s", out)
	}
	if got := reg.ArtifactPersistFailures(metrics.ArtifactEvidence); got != 2 {
		t.Fatalf("ArtifactPersistFailures(evidence) = %d, want 2", got)
	}
}

func TestArtifactPersistFailureIsNilSafe(t *testing.T) {
	var reg *metrics.Registry
	reg.IncArtifactPersistFailure(metrics.ArtifactVote) // must not panic
	if got := reg.ArtifactPersistFailures(metrics.ArtifactVote); got != 0 {
		t.Fatalf("nil registry returned %d, want 0", got)
	}
}
