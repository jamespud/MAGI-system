package admin_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubAdminCases struct {
	mu    sync.Mutex
	cases []*entity.DecisionCase
}

func (s *stubAdminCases) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *stubAdminCases) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *stubAdminCases) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cases, nil
}
func (s *stubAdminCases) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *stubAdminCases) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *stubAdminCases) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*entity.DecisionCase, 0, len(s.cases))
	for _, c := range s.cases {
		if userID == 0 || c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, int64(len(out)), nil
}
func (s *stubAdminCases) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *stubAdminCases) Delete(ctx context.Context, id string) error { return nil }

type stubAdminRuns struct {
	mu   sync.Mutex
	runs map[string][]*entity.AgentRun
}

func (s *stubAdminRuns) Create(ctx context.Context, r *entity.AgentRun) error { return nil }
func (s *stubAdminRuns) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	return nil, nil
}
func (s *stubAdminRuns) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[caseID], nil
}
func (s *stubAdminRuns) SumUsageByUser(ctx context.Context, userID int64) (int64, float64, error) {
	return 0, 0, nil
}
func (s *stubAdminRuns) CountByUser(ctx context.Context, userID int64) (int64, error) { return 0, nil }

func TestAdminUsage_AggregatesPerUser(t *testing.T) {
	cases := &stubAdminCases{cases: []*entity.DecisionCase{
		{ID: "c1", UserID: 7},
		{ID: "c2", UserID: 7},
		{ID: "c3", UserID: 8},
	}}
	runs := &stubAdminRuns{runs: map[string][]*entity.AgentRun{
		"c1": {{Usage: &entity.Usage{TotalTokens: 100, CostUSD: 0.5}}},
		"c2": {{Usage: &entity.Usage{TotalTokens: 50, CostUSD: 0.25}}},
		"c3": {},
	}}
	svc := admin.NewService(cases, runs)
	sum, err := svc.Usage(context.Background())
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if sum.TotalCases != 3 || sum.TotalRuns != 2 || sum.TotalTokens != 150 || sum.TotalCostUSD != 0.75 {
		t.Fatalf("totals: %+v", sum)
	}
	if len(sum.ByUser) != 2 {
		t.Fatalf("by user: %+v", sum.ByUser)
	}
}

func TestService_UserUsageAggregates(t *testing.T) {
	svc := admin.NewService(&stubAdminCases{cases: []*entity.DecisionCase{
		{ID: "c1", UserID: 7, Question: "q"},
		{ID: "c2", UserID: 7, Question: "q2"},
		{ID: "c3", UserID: 9, Question: "q3"},
	}}, &stubBudgetRuns{tokens: 5000, cost: 3.5})
	ctx := context.Background()

	row, err := svc.UserUsage(ctx, 7)
	if err != nil {
		t.Fatalf("user usage: %v", err)
	}
	if row == nil {
		t.Fatal("row is nil")
	}
	if row.Cases != 2 {
		t.Fatalf("expected 2 cases for user 7, got %d", row.Cases)
	}
	if row.Tokens != 5000 || row.CostUSD != 3.5 {
		t.Fatalf("usage totals: tokens=%d cost=%.2f", row.Tokens, row.CostUSD)
	}
}
