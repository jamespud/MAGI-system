package metrics_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestRegistry_RunDurationHistogramAndCost(t *testing.T) {
	reg := metrics.New()
	reg.RecordRunDuration(120)
	reg.RecordRunDuration(5000)
	reg.AddCostUSD(0.25)

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()
	for _, want := range []string{
		"magi_run_duration_ms_count 2",
		"magi_run_duration_ms_sum 5120",
		`magi_run_duration_ms_bucket{le="100"} 0`,
		`magi_run_duration_ms_bucket{le="500"} 1`,
		`magi_run_duration_ms_bucket{le="5000"} 2`,
		"magi_cost_usd_total 0.250000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}
