package decision_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubOrchestrator struct {
	result *entity.Resolution
	err    error
}

func (s *stubOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	return s.result, s.err
}

func TestService_Create(t *testing.T) {
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{MaxDebateRounds: 2})
	c, err := svc.Create(context.Background(), "test question")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Question != "test question" {
		t.Fatalf("question: %s", c.Question)
	}
	if c.MaxDebateRounds != 2 {
		t.Fatalf("maxDebateRounds: %d", c.MaxDebateRounds)
	}
	if c.Status != entity.CaseStatusDraft {
		t.Fatalf("status: %s", c.Status)
	}
}

func TestService_Run(t *testing.T) {
	want := &entity.Resolution{FinalDecision: entity.VoteDecisionApprove}
	svc := decision.NewService(&stubOrchestrator{result: want}, decision.ServiceConfig{MaxDebateRounds: 2})
	c, _ := svc.Create(context.Background(), "q")
	got, err := svc.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.FinalDecision != want.FinalDecision {
		t.Fatalf("decision: %s", got.FinalDecision)
	}
}
