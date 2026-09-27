package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
)

// autoApplyGate sequences an automated regression run and the prompt publish it
// guards. The previous wiring called AutoApply immediately after *starting* the
// regression, so the publish raced the run and a failing verdict could not
// block it. This gate waits for the persisted verdict and publishes only when
// that verdict explicitly permits it.
type autoApplyGate struct {
	startRegression func(ctx context.Context) (*entity.BenchmarkRun, error)
	awaitRegression func(ctx context.Context, runID string) (*entity.BenchmarkRun, error)
	apply           func(ctx context.Context) (int, error)
	// timeout bounds the wait for the persisted verdict. A zero value waits no
	// longer than the caller's context.
	timeout time.Duration
	metrics *metrics.Registry
}

// run returns the number of applied suggestions, the fixed block reason when
// the publish was refused, and the underlying error, if any. A verdict that
// refuses the publish is an ordinary outcome and returns a nil error; err is
// reserved for the regression or the publish actually failing.
func (g autoApplyGate) run(ctx context.Context) (applied int, blocked metrics.AutoApplyBlockReason, err error) {
	run, err := g.startRegression(ctx)
	if err != nil {
		// Another replica already has a regression in flight, so no verdict is
		// available for this tick; that is "no result", not "failed result".
		reason := metrics.AutoApplyBlockedRegressionError
		if errors.Is(err, dataset.ErrRunActive) {
			reason = metrics.AutoApplyBlockedIncomplete
		}
		g.recordBlock(reason)
		return 0, reason, fmt.Errorf("start auto regression: %w", err)
	}
	if run == nil {
		g.recordBlock(metrics.AutoApplyBlockedIncomplete)
		return 0, metrics.AutoApplyBlockedIncomplete, errors.New("start auto regression returned no run")
	}

	awaitCtx := ctx
	if g.timeout > 0 {
		var cancel context.CancelFunc
		awaitCtx, cancel = context.WithTimeout(ctx, g.timeout)
		defer cancel()
	}
	verdict, err := g.awaitRegression(awaitCtx, run.ID)
	if err != nil {
		reason := metrics.AutoApplyBlockedRegressionError
		if errors.Is(err, context.DeadlineExceeded) {
			reason = metrics.AutoApplyBlockedTimeout
		}
		g.recordBlock(reason)
		return 0, reason, fmt.Errorf("await auto regression %s: %w", run.ID, err)
	}

	allowed, reason := regressionAllowsAutoApply(verdict)
	if !allowed {
		g.recordBlock(reason)
		// A verdict that refuses the publish is an ordinary outcome: the caller
		// logs the bounded reason rather than treating it as a failure.
		return 0, reason, nil
	}

	applied, err = g.apply(ctx)
	if err != nil {
		// The verdict permitted the publish; the publish itself failed. That is
		// a different operational event, so it must not be counted as a block.
		return 0, "", fmt.Errorf("apply suggestions: %w", err)
	}
	return applied, "", nil
}

func (g autoApplyGate) recordBlock(reason metrics.AutoApplyBlockReason) {
	if g.metrics != nil {
		g.metrics.IncAutoApplyBlocked(reason)
	}
}

// regressionAllowsAutoApply is the admission rule for an automated publish. It
// permits the publish only for an explicit, interpretable pass: the run must be
// finished and succeeded, must not have failed its regression threshold, must
// have evaluated at least one sample, and must carry a configured threshold.
// Everything else — a missing or unfinished run, a failed threshold, item
// errors, or a run that cannot be interpreted — blocks.
//
// Passing this gate only means the regression did not regress. It does not
// validate the candidate prompt that is about to be published; version binding,
// independent calibration and compatibility acceptance remain separate work.
func regressionAllowsAutoApply(run *entity.BenchmarkRun) (bool, metrics.AutoApplyBlockReason) {
	switch {
	case run == nil:
		return false, metrics.AutoApplyBlockedIncomplete
	case run.Status == entity.BenchmarkRunQueued || run.Status == entity.BenchmarkRunRunning:
		return false, metrics.AutoApplyBlockedIncomplete
	case run.RegressionFailed:
		return false, metrics.AutoApplyBlockedRegressionFailed
	case run.Status != entity.BenchmarkRunSucceeded:
		// A failed status without a regression flag means the run itself hit
		// execution or item errors, so there is no trustworthy verdict.
		return false, metrics.AutoApplyBlockedRegressionError
	case run.Total <= 0:
		return false, metrics.AutoApplyBlockedIncomplete
	case run.RegressionThreshold <= 0:
		return false, metrics.AutoApplyBlockedIncomplete
	}
	return true, ""
}
