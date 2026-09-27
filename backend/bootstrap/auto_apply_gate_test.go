package bootstrap

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
)

func queuedRun() *entity.BenchmarkRun {
	return &entity.BenchmarkRun{ID: "bench-1", Status: entity.BenchmarkRunQueued}
}

// Issue #12: publishing must be gated on a persisted, finished, passing
// verdict, not on the still-queued object the start call returns.
func TestAutoApplyGate_BlocksPublishWhenRegressionFailed(t *testing.T) {
	reg := metrics.New()
	applyCalls := 0
	gate := autoApplyGate{
		startRegression: func(context.Context) (*entity.BenchmarkRun, error) { return queuedRun(), nil },
		awaitRegression: func(context.Context, string) (*entity.BenchmarkRun, error) {
			return &entity.BenchmarkRun{
				ID: "bench-1", Status: entity.BenchmarkRunFailed, Total: 4, Matched: 2,
				RegressionThreshold: 0.9, RegressionFailed: true, FailureReason: "accuracy 0.50 below threshold 0.90",
			}, nil
		},
		apply:   func(context.Context) (int, error) { applyCalls++; return 1, nil },
		metrics: reg,
	}

	applied, blocked, err := gate.run(context.Background())
	if err != nil {
		t.Fatalf("gate run: %v", err)
	}
	if applied != 0 || applyCalls != 0 {
		t.Fatalf("a failed regression must not publish: applied=%d applyCalls=%d", applied, applyCalls)
	}
	if blocked != metrics.AutoApplyBlockedRegressionFailed {
		t.Fatalf("block reason = %q", blocked)
	}
	if got := reg.AutoApplyBlocked(metrics.AutoApplyBlockedRegressionFailed); got != 1 {
		t.Fatalf("blocked metric = %d", got)
	}
}

func TestAutoApplyGate_PublishesAfterPassingRegression(t *testing.T) {
	reg := metrics.New()
	applyCalls := 0
	gate := autoApplyGate{
		startRegression: func(context.Context) (*entity.BenchmarkRun, error) { return queuedRun(), nil },
		awaitRegression: func(context.Context, string) (*entity.BenchmarkRun, error) {
			return &entity.BenchmarkRun{ID: "bench-1", Status: entity.BenchmarkRunSucceeded, Total: 4, Matched: 4, RegressionThreshold: 0.9}, nil
		},
		apply:   func(context.Context) (int, error) { applyCalls++; return 2, nil },
		metrics: reg,
	}

	applied, blocked, err := gate.run(context.Background())
	if err != nil || blocked != "" || applied != 2 || applyCalls != 1 {
		t.Fatalf("passing verdict must publish: applied=%d blocked=%q err=%v calls=%d", applied, blocked, err, applyCalls)
	}
}

func TestAutoApplyGate_TreatsTimeoutAndActiveRunAsBlocks(t *testing.T) {
	timeoutReg := metrics.New()
	timeout := autoApplyGate{
		startRegression: func(context.Context) (*entity.BenchmarkRun, error) { return queuedRun(), nil },
		awaitRegression: func(context.Context, string) (*entity.BenchmarkRun, error) {
			return nil, context.DeadlineExceeded
		},
		apply:   func(context.Context) (int, error) { t.Fatal("timeout must not publish"); return 0, nil },
		metrics: timeoutReg,
	}
	if _, blocked, err := timeout.run(context.Background()); blocked != metrics.AutoApplyBlockedTimeout || err == nil {
		t.Fatalf("timeout: blocked=%q err=%v", blocked, err)
	}

	activeReg := metrics.New()
	active := autoApplyGate{
		startRegression: func(context.Context) (*entity.BenchmarkRun, error) { return nil, dataset.ErrRunActive },
		awaitRegression: func(context.Context, string) (*entity.BenchmarkRun, error) {
			t.Fatal("an active run must not be awaited")
			return nil, nil
		},
		apply:   func(context.Context) (int, error) { t.Fatal("an active run must not publish"); return 0, nil },
		metrics: activeReg,
	}
	if _, blocked, err := active.run(context.Background()); blocked != metrics.AutoApplyBlockedIncomplete || err == nil {
		t.Fatalf("active run: blocked=%q err=%v", blocked, err)
	}
}

func TestRegressionAllowsAutoApply(t *testing.T) {
	cases := []struct {
		name    string
		run     *entity.BenchmarkRun
		allowed bool
		reason  metrics.AutoApplyBlockReason
	}{
		{name: "missing run", run: nil, reason: metrics.AutoApplyBlockedIncomplete},
		{name: "still queued", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunQueued, Total: 4, RegressionThreshold: 0.9}, reason: metrics.AutoApplyBlockedIncomplete},
		{name: "still running", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunRunning, Total: 4, RegressionThreshold: 0.9}, reason: metrics.AutoApplyBlockedIncomplete},
		{name: "threshold missed", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunFailed, Total: 4, RegressionFailed: true, RegressionThreshold: 0.9}, reason: metrics.AutoApplyBlockedRegressionFailed},
		{name: "item errors", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunFailed, Total: 4, RegressionThreshold: 0.9}, reason: metrics.AutoApplyBlockedRegressionError},
		{name: "no samples", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunSucceeded, Total: 0, RegressionThreshold: 0.9}, reason: metrics.AutoApplyBlockedIncomplete},
		{name: "no threshold", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunSucceeded, Total: 4}, reason: metrics.AutoApplyBlockedIncomplete},
		{name: "passed", run: &entity.BenchmarkRun{Status: entity.BenchmarkRunSucceeded, Total: 4, Matched: 4, RegressionThreshold: 0.9}, allowed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, reason := regressionAllowsAutoApply(tc.run)
			if allowed != tc.allowed || reason != tc.reason {
				t.Fatalf("allowed=%v reason=%q, want allowed=%v reason=%q", allowed, reason, tc.allowed, tc.reason)
			}
		})
	}
}

func TestAutoApplyGate_DoesNotSwallowApplyErrors(t *testing.T) {
	reg := metrics.New()
	gate := autoApplyGate{
		startRegression: func(context.Context) (*entity.BenchmarkRun, error) { return queuedRun(), nil },
		awaitRegression: func(context.Context, string) (*entity.BenchmarkRun, error) {
			return &entity.BenchmarkRun{ID: "bench-1", Status: entity.BenchmarkRunSucceeded, Total: 4, RegressionThreshold: 0.9}, nil
		},
		apply:   func(context.Context) (int, error) { return 0, errors.New("prompt registry unavailable") },
		metrics: reg,
	}
	applied, blocked, err := gate.run(context.Background())
	if err == nil || applied != 0 {
		t.Fatalf("apply failure must be reported: applied=%d blocked=%q err=%v", applied, blocked, err)
	}
	if blocked != "" {
		t.Fatalf("an apply failure is not a regression block, got %q", blocked)
	}
}
