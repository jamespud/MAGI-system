package assistant_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

type askCaseRepo struct {
	mu sync.Mutex
}

func (s *askCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *askCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *askCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *askCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *askCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

func (s *askCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *askCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error { return nil }
func (s *askCaseRepo) Delete(ctx context.Context, id string) error { return nil }

type askOrch struct{}

func (askOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	c.Status = entity.CaseStatusResolved
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove, FinalReport: "report"}, nil
}

func TestAsk_RunsDecisionAndReturnsResolution(t *testing.T) {
	dec := decision.NewService(askOrch{}, decision.ServiceConfig{MaxDebateRounds: 1}, decision.WithCaseRepo(&askCaseRepo{}))
	svc := assistant.NewService(dec)
	cs, err := svc.AskAsync(context.Background(), 7, "Should we adopt Rust?", "", nil)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if cs.UserID != 7 {
		t.Fatalf("case owner: %d", cs.UserID)
	}
}

func TestAsk_RequiresMessage(t *testing.T) {
	dec := decision.NewService(askOrch{}, decision.ServiceConfig{})
	svc := assistant.NewService(dec)
	if _, err := svc.AskAsync(context.Background(), 1, "", "", nil); err == nil {
		t.Fatal("expected error for empty message")
	}
}
