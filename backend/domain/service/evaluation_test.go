package service_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
)

type stubOrchestrator struct {
	decisions []entity.VoteDecision
	calls     int
}

func (s *stubOrchestrator) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	d := entity.VoteDecisionApprove
	if s.calls < len(s.decisions) {
		d = s.decisions[s.calls]
	}
	s.calls++
	return &entity.Resolution{FinalDecision: d}, nil
}

func TestCounterfactualStability_AllSame(t *testing.T) {
	orch := &stubOrchestrator{decisions: []entity.VoteDecision{
		entity.VoteDecisionApprove, entity.VoteDecisionApprove, entity.VoteDecisionApprove,
	}}
	stab := service.CounterfactualStability(context.Background(),
		&entity.DecisionCase{ID: "c1"}, orch, 3)
	if stab != 1.0 {
		t.Fatalf("expected 1.0, got %v", stab)
	}
}

func TestCounterfactualStability_Mixed(t *testing.T) {
	orch := &stubOrchestrator{decisions: []entity.VoteDecision{
		entity.VoteDecisionApprove, entity.VoteDecisionReject, entity.VoteDecisionApprove,
	}}
	stab := service.CounterfactualStability(context.Background(),
		&entity.DecisionCase{ID: "c1"}, orch, 3)
	if stab != 2.0/3.0 {
		t.Fatalf("expected 0.667, got %v", stab)
	}
}

func TestEvaluate_PopulatesAllCategories(t *testing.T) {
	results := []*runtime.LoopResult{
		{Status: runtime.LoopStatusGateFailed},
		{Status: runtime.LoopStatusCompleted, Usage: &entity.Usage{TotalTokens: 100}},
	}
	ev := service.Evaluate(results, 2, entity.ConsensusMajorityApprovalDissent)
	if ev == nil {
		t.Fatal("nil evaluation")
	}
	if ev.GateFailures != 1 {
		t.Fatalf("GateFailures: got %d, want 1", ev.GateFailures)
	}
	if ev.TotalTokens != 100 {
		t.Fatalf("TotalTokens: got %d, want 100", ev.TotalTokens)
	}
	if ev.ConsensusRound != 2 {
		t.Fatalf("ConsensusRound: got %d, want 2", ev.ConsensusRound)
	}
	if ev.FirstRoundConsensus {
		t.Fatal("round 2 should not be first-round consensus")
	}
}
