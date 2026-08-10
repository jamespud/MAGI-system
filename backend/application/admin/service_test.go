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
