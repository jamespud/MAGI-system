package metrics_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestToolEffectOverridesAreExported(t *testing.T) {
	reg := metrics.New()
	reg.IncToolEffectOverride(metrics.ToolEffectUnknown, metrics.ToolEffectReadOnly)
	reg.IncToolEffectOverride(metrics.ToolEffectUnknown, metrics.ToolEffectIdempotent)
	reg.IncToolEffectOverride(metrics.ToolEffectUnknown, metrics.ToolEffectIdempotent)
	// A label outside the fixed set must be dropped, not create a new series.
	reg.IncToolEffectOverride(metrics.ToolEffectClass("yolo"), metrics.ToolEffectReadOnly)

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()

	if !strings.Contains(out, `magi_tool_effect_override_total{from="unknown",to="read_only"} 1`) {
		t.Fatalf("override series missing or wrong:\n%s", out)
	}
	if !strings.Contains(out, `magi_tool_effect_override_total{from="unknown",to="idempotent"} 2`) {
		t.Fatalf("override series missing or wrong:\n%s", out)
	}
	// Zero-value pairs are exposed too: the series exists to answer an audit
	// question after the fact, so a dashboard must be able to rely on it.
	if !strings.Contains(out, `magi_tool_effect_override_total{from="read_only",to="read_only"} 0`) {
		t.Fatalf("zero-value series missing:\n%s", out)
	}
	if strings.Contains(out, "yolo") {
		t.Fatalf("an unknown label value created a series:\n%s", out)
	}
	if got := reg.ToolEffectOverrides(metrics.ToolEffectUnknown, metrics.ToolEffectIdempotent); got != 2 {
		t.Fatalf("ToolEffectOverrides(unknown, idempotent) = %d, want 2", got)
	}
}

func TestToolEffectOverrideIsNilSafe(t *testing.T) {
	var reg *metrics.Registry
	reg.IncToolEffectOverride(metrics.ToolEffectUnknown, metrics.ToolEffectReadOnly) // must not panic
	if got := reg.ToolEffectOverrides(metrics.ToolEffectUnknown, metrics.ToolEffectReadOnly); got != 0 {
		t.Fatalf("nil registry returned %d, want 0", got)
	}
}
