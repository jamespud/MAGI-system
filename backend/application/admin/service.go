package admin

import (
	"context"
	"fmt"
	"sort"

	"github.com/jamespud/magi/backend/domain/port"
)

// UsageRow aggregates a single user's harness usage.
type UsageRow struct {
	UserID  int64
	Cases   int
	Runs    int
	Tokens  int64
	CostUSD float64
}

// UsageLimits carries the configured per-user budget ceilings for display.
type UsageLimits struct {
	MaxTokens  int64
	MaxCostUSD float64
}

// UsageSummary is the admin-facing aggregate across all users.
type UsageSummary struct {
	TotalCases   int
	TotalRuns    int
	TotalTokens  int64
	TotalCostUSD float64
	ByUser       []UsageRow
}

// Budget is the per-user cumulative allowance enforced before a run starts.
// Zero values mean that dimension is unlimited.
type Budget struct {
	MaxTokens   int64
	MaxCostUSD  float64
	UsedTokens  int64
	UsedCostUSD float64
}

// Exceeds reports whether the budget has been exhausted on either dimension.
func (b *Budget) Exceeds() (tokens bool, cost bool) {
	if b == nil {
		return false, false
	}
	if b.MaxTokens > 0 {
		tokens = b.UsedTokens >= b.MaxTokens
	}
	if b.MaxCostUSD > 0 {
		cost = b.UsedCostUSD >= b.MaxCostUSD
	}
	return tokens, cost
}

// Service exposes admin-only aggregates over harness telemetry.
type Service struct {
	cases port.CaseRepository
	runs  port.AgentRunRepository
}

func NewService(cases port.CaseRepository, runs port.AgentRunRepository) *Service {
	return &Service{cases: cases, runs: runs}
}

// Usage aggregates persisted agent runs per owner, including token usage and
// estimated cost captured at run time.
// Usage aggregates persisted agent runs per owner, including token usage and
// estimated cost captured at run time. The production path aggregates in SQL
// (COUNT/GROUP BY + JSON_EXTRACT) so a large case history is never fully
// loaded into memory (OOM fix); repositories that do not implement the
// aggregation capabilities fall back to the legacy full-list + N+1 loop.
func (s *Service) Usage(ctx context.Context) (*UsageSummary, error) {
	if s.cases == nil || s.runs == nil {
		return nil, fmt.Errorf("admin: repositories are not configured")
	}
	if cases, ok := s.cases.(port.CaseUsageByUser); ok {
		if runs, ok2 := s.runs.(port.RunUsageByUser); ok2 {
			return usageAggregated(ctx, cases, runs)
		}
	}
	return s.usageLegacy(ctx)
}

// usageAggregated builds the usage summary from per-owner SQL aggregation.
func usageAggregated(ctx context.Context, cases port.CaseUsageByUser, runs port.RunUsageByUser) (*UsageSummary, error) {
	caseCounts, err := cases.CountCasesByUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: count cases: %w", err)
	}
	runAggs, err := runs.UsageByUserGrouped(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: aggregate runs: %w", err)
	}
	ids := make(map[int64]bool, len(caseCounts)+len(runAggs))
	for uid := range caseCounts {
		ids[uid] = true
	}
	for uid := range runAggs {
		ids[uid] = true
	}
	userIDs := make([]int64, 0, len(ids))
	for uid := range ids {
		userIDs = append(userIDs, uid)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })

	sum := &UsageSummary{}
	for _, uid := range userIDs {
		row := UsageRow{UserID: uid, Cases: int(caseCounts[uid])}
		if agg, ok := runAggs[uid]; ok {
			row.Runs = int(agg.Runs)
			row.Tokens = agg.Tokens
			row.CostUSD = agg.Cost
		}
		sum.TotalCases += row.Cases
		sum.TotalRuns += row.Runs
		sum.TotalTokens += row.Tokens
		sum.TotalCostUSD += row.CostUSD
		sum.ByUser = append(sum.ByUser, row)
	}
	return sum, nil
}

// usageLegacy aggregates in memory from the full case list and per-case runs.
// It is retained for test/in-memory repositories that do not implement the
// SQL aggregation capabilities.
func (s *Service) usageLegacy(ctx context.Context) (*UsageSummary, error) {
	cases, err := s.cases.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("admin: list cases: %w", err)
	}
	sum := &UsageSummary{}
	byUser := map[int64]*UsageRow{}
	order := []int64{}
	for _, c := range cases {
		if c == nil {
			continue
		}
		sum.TotalCases++
		row := byUser[c.UserID]
		if row == nil {
			row = &UsageRow{UserID: c.UserID}
			byUser[c.UserID] = row
			order = append(order, c.UserID)
		}
		row.Cases++
		runs, err := s.runs.ListByCase(ctx, c.ID)
		if err != nil {
			return nil, fmt.Errorf("admin: list runs for %s: %w", c.ID, err)
		}
		for _, r := range runs {
			if r == nil {
				continue
			}
			row.Runs++
			sum.TotalRuns++
			if r.Usage != nil {
				row.Tokens += r.Usage.TotalTokens
				row.CostUSD += r.Usage.CostUSD
				sum.TotalTokens += r.Usage.TotalTokens
				sum.TotalCostUSD += r.Usage.CostUSD
			}
		}
	}
	for _, uid := range order {
		sum.ByUser = append(sum.ByUser, *byUser[uid])
	}
	return sum, nil
}

// UserUsage aggregates one user's cases, runs, tokens, and cost for the
// /me/usage endpoint (P2 D9). It is efficient: case count uses a COUNT query
// and run/token totals come from the same join the budget path uses.
func (s *Service) UserUsage(ctx context.Context, userID int64) (*UsageRow, error) {
	row := &UsageRow{UserID: userID}
	if s.cases != nil {
		if _, total, err := s.cases.ListPaged(ctx, userID, 1, 1); err == nil {
			row.Cases = int(total)
		}
	}
	if s.runs != nil {
		if n, err := s.runs.CountByUser(ctx, userID); err == nil {
			row.Runs = int(n)
		}
		tokens, cost, err := s.runs.SumUsageByUser(ctx, userID)
		if err == nil {
			row.Tokens = tokens
			row.CostUSD = cost
		}
	}
	return row, nil
}

// Budget reports a user's cumulative usage and whether it exceeds the given
// per-user allowances.
func (s *Service) Budget(ctx context.Context, userID int64, maxTokens int64, maxCostUSD float64) (*Budget, error) {
	b := &Budget{MaxTokens: maxTokens, MaxCostUSD: maxCostUSD}
	if s.runs == nil {
		return nil, fmt.Errorf("budget: agent run repository is not configured")
	}
	usedTokens, usedCost, err := s.runs.SumUsageByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("budget: sum usage: %w", err)
	}
	b.UsedTokens = usedTokens
	b.UsedCostUSD = usedCost
	return b, nil
}
