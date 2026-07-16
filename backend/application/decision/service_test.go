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

type stubCaseRepo struct {
	case_       *entity.DecisionCase
	cancelledID string
}

func (s *stubCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *stubCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.case_, nil
}
func (s *stubCaseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	s.cancelledID = id
	return nil
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

func TestService_Get(t *testing.T) {
	repo := &stubCaseRepo{case_: &entity.DecisionCase{ID: "c1", Question: "found"}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	got, err := svc.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != "c1" {
		t.Fatalf("expected c1, got: %+v", got)
	}
}

func TestService_Cancel(t *testing.T) {
	repo := &stubCaseRepo{}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	err := svc.Cancel(context.Background(), "c1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if repo.cancelledID != "c1" {
		t.Fatalf("expected cancel c1, got %s", repo.cancelledID)
	}
}
