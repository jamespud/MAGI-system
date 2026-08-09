package dataset_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
)

// sequenceOrch returns one decision per Orchestrate call in order.
type sequenceOrch struct {
	mu        sync.Mutex
	decisions []entity.VoteDecision
	calls     int
}

func (s *sequenceOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.decisions[s.calls%len(s.decisions)]
	s.calls++
	return &entity.Resolution{CaseID: c.ID, FinalDecision: d}, nil
}

func TestService_RunCounterfactualStability(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &sequenceOrch{decisions: []entity.VoteDecision{
		entity.VoteDecisionApprove, entity.VoteDecisionApprove, entity.VoteDecisionReject,
	}}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "stability", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
	})
	run, err := svc.StartRunWithOptions(ctx, 0, d.ID, dataset.RunOptions{RunsPerItem: 3})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.RunsPerItem != 3 || run.Total != 1 {
		t.Fatalf("run: %+v", run)
	}
	if run.Stability != 2.0/3.0 {
		t.Fatalf("stability: %v", run.Stability)
	}
	results, _ := repo.ListItemResults(ctx, run.ID)
	if len(results) != 1 {
		t.Fatalf("results: %d", len(results))
	}
	r := results[0]
	if r.Runs != 3 || r.ActualDecision != entity.VoteDecisionApprove || r.Consistency != 2.0/3.0 || !r.Matched {
		t.Fatalf("result: %+v", r)
	}
	if len(r.Decisions) != 3 {
		t.Fatalf("decisions: %v", r.Decisions)
	}
}

func TestService_RunRegressionGateFailsRun(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &sequenceOrch{decisions: []entity.VoteDecision{
		entity.VoteDecisionApprove, entity.VoteDecisionReject,
	}}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1, dataset.WithRegressionThreshold(0.8))
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "gate", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "q2", ExpectedDecision: entity.VoteDecisionApprove},
	})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunFailed)
	if !run.RegressionFailed {
		t.Fatalf("expected regression failure: %+v", run)
	}
	if run.FailureReason == "" {
		t.Fatal("expected failure reason")
	}
}

func TestService_RunRegressionGatePasses(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &sequenceOrch{decisions: []entity.VoteDecision{entity.VoteDecisionApprove}}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1, dataset.WithRegressionThreshold(0.8))
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "gate-ok", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "q2", ExpectedDecision: entity.VoteDecisionApprove},
	})
	run, err := svc.StartRun(ctx, 0, d.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.RegressionFailed || run.Accuracy != 1.0 {
		t.Fatalf("run: %+v", run)
	}
}
