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
