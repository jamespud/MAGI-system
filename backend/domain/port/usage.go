package port

import "context"

// UsageByUserRow is one owner's aggregated usage row (admin.Usage fast path).
type UsageByUserRow struct {
	Runs   int64
	Tokens int64
	Cost   float64
}

// CaseUsageByUser is an optional CaseRepository capability that counts cases
// per owner in SQL. The GORM repository implements it so admin.Usage never
// materializes the whole case table into memory (OOM fix for large histories).
type CaseUsageByUser interface {
	CountCasesByUser(ctx context.Context) (map[int64]int64, error)
}

// RunUsageByUser is an optional AgentRunRepository capability that aggregates
// runs/tokens/cost per owner in SQL (single GROUP BY instead of an N+1 loop of
// ListByCase queries).
type RunUsageByUser interface {
	UsageByUserGrouped(ctx context.Context) (map[int64]UsageByUserRow, error)
}
