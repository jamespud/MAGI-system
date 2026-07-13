package service_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
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
