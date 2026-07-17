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

// EvaluateCase evaluates a completed case by ID. Phase 5: returns empty
// evaluation (real data wiring requires fetching results from a case, future work).
func (s *Service) EvaluateCase(ctx context.Context, caseID string) (*entity.Evaluation, error) {
	return domainservice.Evaluate(nil, 0, entity.ConsensusOutcome("")), nil
}

// Compare compares evaluations of two cases (future: fetches both and diffs).
func (s *Service) Compare(ctx context.Context, caseA, caseB string) (map[string]*entity.Evaluation, error) {
	evA, _ := s.EvaluateCase(ctx, caseA)
	evB, _ := s.EvaluateCase(ctx, caseB)
	return map[string]*entity.Evaluation{"a": evA, "b": evB}, nil
}

// Benchmark runs evaluation across multiple cases (future: batch fetch).
func (s *Service) Benchmark(ctx context.Context, caseIDs []string) (map[string]*entity.Evaluation, error) {
	result := make(map[string]*entity.Evaluation, len(caseIDs))
	for _, id := range caseIDs {
		ev, _ := s.EvaluateCase(ctx, id)
		result[id] = ev
	}
	return result, nil
}
