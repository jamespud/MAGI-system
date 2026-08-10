package admin

import (
	"context"
	"fmt"

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
func (s *Service) Usage(ctx context.Context) (*UsageSummary, error) {
	if s.cases == nil || s.runs == nil {
		return nil, fmt.Errorf("admin: repositories are not configured")
	}
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
