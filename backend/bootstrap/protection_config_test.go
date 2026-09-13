package bootstrap

import (
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
)

// A configuration that omits every protection must be visible as such: this is
// the programmatic form of the SECURITY log line (see
// docs/reliability-hazard-audit.md §4.1).
func TestRecordDisabledProtections_AllOff(t *testing.T) {
	reg := metrics.New()
	recordDisabledProtections(&Config{}, reg)

	got := reg.DisabledProtections()
	want := map[metrics.ProtectionName]bool{
		metrics.ProtectionAuth: false, metrics.ProtectionBudget: false,
		metrics.ProtectionToolQuota: false, metrics.ProtectionHTTPRateLimit: false,
		metrics.ProtectionBenchmarkGate: false,
	}
	for _, name := range got {
		if _, ok := want[name]; !ok {
			t.Fatalf("unexpected protection reported disabled: %q", name)
		}
		want[name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("protection %q was not reported as disabled", name)
		}
	}
}

func TestRecordDisabledProtections_AllOn(t *testing.T) {
	cfg := &Config{}
	cfg.Auth.Enabled = true
	cfg.Limits.MaxConcurrentRunsPerUser = 3
	cfg.Limits.MaxTokensPerUser = 1_000_000
	cfg.ToolQuota.DefaultPerMinute = 60
	cfg.HTTPRateLimit.Enabled = true
	cfg.HTTPRateLimit.PerUserPerMinute = 60
	cfg.Benchmark.RegressionThreshold = 0.7

	reg := metrics.New()
	recordDisabledProtections(cfg, reg)
	if got := reg.DisabledProtections(); len(got) != 0 {
		t.Fatalf("fully configured protections reported as disabled: %v", got)
	}
}

func TestToolQuotaEnabled(t *testing.T) {
	cfg := &Config{}
	if toolQuotaEnabled(cfg) {
		t.Fatal("zero default quota reported as enabled")
	}
	cfg.ToolQuota.Tools = map[string]int{"code_runner": 5}
	if !toolQuotaEnabled(cfg) {
		t.Fatal("per-tool quota not counted as enabled")
	}
}
