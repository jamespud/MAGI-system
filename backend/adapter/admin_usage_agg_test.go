package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/domain/entity"
)

// TestAdminUsage_AggregatedSQL verifies the SQL-aggregation fast path
// (CountCasesByUser + UsageByUserGrouped) produces correct per-user totals
// without materializing the case table (the OOM fix).
func TestAdminUsage_AggregatedSQL(t *testing.T) {
	db := openDatasetDB(t)
	ctx := context.Background()
	repo := magi.NewRepository(db)
	cr := repo.CaseRepo()
	ar := repo.AgentRunRepo()

	now := time.Now()
	for _, id := range []string{"case-a", "case-b", "case-c"} {
		if err := cr.Create(ctx, &entity.DecisionCase{ID: id, UserID: 1, Question: "q " + id, Status: entity.CaseStatusResolved}); err != nil {
			t.Fatalf("create case: %v", err)
		}
	}
	if err := cr.Create(ctx, &entity.DecisionCase{ID: "case-d", UserID: 2, Question: "q d", Status: entity.CaseStatusResolved}); err != nil {
		t.Fatalf("create case d: %v", err)
	}

	seedRun := func(id, caseID string, tokens int64, cost float64) {
		t.Helper()
		completed := now
		if err := ar.Create(ctx, &entity.AgentRun{
			ID: id, CaseID: caseID, MagiCode: entity.MagiCode("melchior"),
			Round: 1, Status: entity.AgentRunStatusCompleted,
			Usage:     &entity.Usage{TotalTokens: tokens, CostUSD: cost},
			StartedAt: now, CompletedAt: &completed,
		}); err != nil {
			t.Fatalf("create run: %v", err)
		}
	}
	seedRun("run-a", "case-a", 100, 0.5)
	seedRun("run-b", "case-b", 50, 0.25)
	seedRun("run-d", "case-d", 200, 1.0)

	svc := admin.NewService(cr, ar)
	sum, err := svc.Usage(ctx)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if sum.TotalCases != 4 || sum.TotalRuns != 3 || sum.TotalTokens != 350 || sum.TotalCostUSD < 1.74 || sum.TotalCostUSD > 1.76 {
		t.Fatalf("totals: cases=%d runs=%d tokens=%d cost=%.2f", sum.TotalCases, sum.TotalRuns, sum.TotalTokens, sum.TotalCostUSD)
	}
	if len(sum.ByUser) != 2 {
		t.Fatalf("byUser = %d rows", len(sum.ByUser))
	}
	if sum.ByUser[0].UserID != 1 {
		t.Fatalf("first row not user1: %+v", sum.ByUser[0])
	}
	row := sum.ByUser[0]
	if row.Cases != 3 || row.Runs != 2 || row.Tokens != 150 || row.CostUSD < 0.74 || row.CostUSD > 0.76 {
		t.Fatalf("user1 row = %+v", row)
	}
}
