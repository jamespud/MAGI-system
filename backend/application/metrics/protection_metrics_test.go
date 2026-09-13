package metrics_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

func TestConfigProtectionDisabledGauge(t *testing.T) {
	reg := metrics.New()
	reg.SetProtectionDisabled(metrics.ProtectionToolQuota)

	var b strings.Builder
	reg.WritePrometheus(&b)
	out := b.String()

	if !strings.Contains(out, `magi_config_protection_disabled{name="tool_quota"} 1`) {
		t.Fatalf("tool_quota gauge missing:\n%s", out)
	}
	// The whole fixed label set is exposed so a dashboard can rely on it.
	if !strings.Contains(out, `magi_config_protection_disabled{name="auth"} 0`) {
		t.Fatalf("zero-value gauge series missing:\n%s", out)
	}
	if got := reg.DisabledProtections(); len(got) != 1 || got[0] != metrics.ProtectionToolQuota {
		t.Fatalf("DisabledProtections = %v", got)
	}
}

func TestSetProtectionDisabledIsNilSafe(t *testing.T) {
	var reg *metrics.Registry
	reg.SetProtectionDisabled(metrics.ProtectionAuth) // must not panic
	if got := reg.DisabledProtections(); got != nil {
		t.Fatalf("nil registry returned %v, want nil", got)
	}
}
