package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Benchmark execution shares admission, Claim, owner fencing and settlement
// with ordinary decisions. Benchmark workers never manufacture an owner.
type benchmarkExecutor struct {
	runs *decision.RunManager
	repo port.Repository
	jobs port.DecisionJobRepository
}

func (*benchmarkExecutor) RequiresExecutionOwner() bool { return true }
func (e *benchmarkExecutor) CheckDecisionWriter(ctx context.Context) error {
	return e.runs.CheckDecisionWriter(ctx)
}
func (e *benchmarkExecutor) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	if err := e.runs.Start(ctx, c); err != nil {
		return nil, err
	}
	if err := e.runs.WaitExecution(ctx, c.ID); err != nil {
		if controls, ok := e.repo.(port.CaseControlCommitter); ok {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_, _ = controls.CommitCaseControl(cleanup, c.ID, entity.CaseStatusCancelled)
		}
		e.runs.CancelLocal(c.ID)
		return nil, err
	}
	job, err := e.jobs.GetByCase(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	current, err := e.repo.CaseRepo().Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if job.Status != entity.DecisionJobSucceeded || current.ExecutionGeneration <= 0 || job.ExecutionGeneration != current.ExecutionGeneration {
		return nil, fmt.Errorf("benchmark decision did not settle successfully: %s", job.Status)
	}
	res, err := e.repo.ResolutionRepo().Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	if res == nil || res.CaseID != c.ID || res.ExecutionGeneration != current.ExecutionGeneration {
		return nil, port.ErrLeaseLost
	}
	return res, nil
}
