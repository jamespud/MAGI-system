package evaluation

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
	domainservice "github.com/jamespud/magi/backend/domain/service"
)

// Service is the application-layer service for evaluation.
type Service struct{}

// NewService creates an EvaluationService.
func NewService() *Service { return &Service{} }

// Evaluate computes evaluation metrics from loop results.
func (s *Service) Evaluate(ctx context.Context, results []*runtime.LoopResult, consensusRound int, consensusOutcome entity.ConsensusOutcome) (*entity.Evaluation, error) {
	return domainservice.Evaluate(results, consensusRound, consensusOutcome), nil
}
