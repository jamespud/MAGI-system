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

// Ask runs a decision synchronously and returns the case with its resolution.
func (s *Service) Ask(ctx context.Context, userID int64, message, background string, constraints []entity.Constraint) (*entity.DecisionCase, *entity.Resolution, error) {
	if message == "" {
		return nil, nil, fmt.Errorf("assistant: message is required")
	}
	case_, err := s.dec.Create(ctx, userID, message, background, constraints)
	if err != nil {
		return nil, nil, fmt.Errorf("assistant: create case: %w", err)
	}
	res, err := s.dec.Run(ctx, case_)
	if err != nil {
		return case_, nil, fmt.Errorf("assistant: decision failed: %w", err)
	}
	return case_, res, nil
}
