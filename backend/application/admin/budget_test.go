package admin_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubBudgetRuns struct {
	tokens int64
	cost   float64
}

func (s *stubBudgetRuns) Create(ctx context.Context, r *entity.AgentRun) error { return nil }
func (s *stubBudgetRuns) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	return nil, nil
}
func (s *stubBudgetRuns) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	return nil, nil
}
func (s *stubBudgetRuns) SumUsageByUser(ctx context.Context, userID int64) (int64, float64, error) {
	return s.tokens, s.cost, nil
}

var _ port.AgentRunRepository = (*stubBudgetRuns)(nil)

func TestService_BudgetEnforcesLimits(t *testing.T) {
	svc := admin.NewService(&stubAdminCases{}, &stubBudgetRuns{tokens: 5000, cost: 3.5})
	ctx := context.Background()

	b, err := svc.Budget(ctx, 7, 0, 0)
	if err != nil {
		t.Fatalf("budget unlimited: %v", err)
	}
	if tok, cost := b.Exceeds(); tok || cost {
		t.Fatalf("unlimited budget must not exceed: %+v", b)
	}

	b, err = svc.Budget(ctx, 7, 4000, 0)
	if err != nil {
		t.Fatalf("budget tokens: %v", err)
	}
	if tok, _ := b.Exceeds(); !tok {
		t.Fatalf("token limit should be exceeded: %+v", b)
	}

	b, err = svc.Budget(ctx, 7, 0, 3.0)
	if err != nil {
		t.Fatalf("budget cost: %v", err)
	}
	if _, cost := b.Exceeds(); !cost {
		t.Fatalf("cost limit should be exceeded: %+v", b)
	}

	b, err = svc.Budget(ctx, 7, 10000, 10.0)
	if err != nil {
		t.Fatalf("budget under: %v", err)
	}
	if tok, cost := b.Exceeds(); tok || cost {
		t.Fatalf("under-limit usage must not exceed: %+v", b)
	}
}
