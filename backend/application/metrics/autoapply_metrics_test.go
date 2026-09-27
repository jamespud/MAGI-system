package metrics_test
 
import (
	"bytes"
	"strings"
	"testing"
 
	"github.com/jamespud/magi/backend/application/metrics"
)
 
// Issue #12: a blocked automated publish must be observable, but the reason
// label must stay a closed set — free-text error detail would make the series
// count unbounded.
func TestAutoApplyBlockedCounterIsBoundedAndLabeled(t *testing.T) {
	reg := metrics.New()
	reg.IncAutoApplyBlocked(metrics.AutoApplyBlockedRegressionFailed)
	reg.IncAutoApplyBlocked(metrics.AutoApplyBlockedRegressionFailed)
	reg.IncAutoApplyBlocked(metrics.AutoApplyBlockReason("dial tcp 10.0.0.1: refused"))
 
	var buf bytes.Buffer
	reg.WritePrometheus(&buf)
	out := buf.String()
 
	if !strings.Contains(out, `magi_selfimprove_autoapply_blocked_total{reason="regression_failed"} 2`) {
		t.Fatalf("blocked counter missing or mislabeled:\n%s", out)
	}
	// Every fixed reason is exposed, including the zero ones, so a blocked
	// publish is distinguishable from a reason that is never wired up.
	for _, reason := range metrics.AutoApplyBlockReasons() {
		want := `magi_selfimprove_autoapply_blocked_total{reason="` + string(reason) + `"}`
		if !strings.Contains(out, want) {
			t.Fatalf("reason %q is not exposed:\n%s", reason, out)
		}
	}
	if strings.Contains(out, "refused") {
		t.Fatalf("unknown reason leaked into a label:\n%s", out)
	}
}
