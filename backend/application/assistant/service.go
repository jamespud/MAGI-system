package assistant

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

// Service is the conversational entry point: a natural-language decision
// question becomes a full MAGI decision run, and the caller receives the
// final decision with its report.
type Service struct {
	dec *decision.Service
}

func NewService(dec *decision.Service) *Service {
	return &Service{dec: dec}
}

// AskAsync creates a case and hands it to the governed async runner
// (concurrency limits, budgets, leases, retries all apply). The caller polls
// the returned case ID for the final resolution.
func (s *Service) AskAsync(ctx context.Context, userID int64, message, background string, constraints []entity.Constraint) (*entity.DecisionCase, error) {
	if message == "" {
		return nil, fmt.Errorf("assistant: message is required")
	}
	case_, err := s.dec.Create(ctx, userID, message, background, constraints)
	if err != nil {
		return nil, fmt.Errorf("assistant: create case: %w", err)
	}
	if err := s.dec.StartRun(ctx, case_); err != nil {
		return case_, fmt.Errorf("assistant: start run: %w", err)
	}
	return case_, nil
}
