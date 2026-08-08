package evaluation

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	domainservice "github.com/jamespud/magi/backend/domain/service"
)

// Service is the application-layer service for evaluation.
type Service struct {
	agentRuns  port.AgentRunRepository
	evidence   port.EvidenceRepository
	votes      port.VoteRepository
	resolution port.ResolutionRepository
	toolCalls  port.ToolCallRepository
}

// Option configures the evaluation service. Repositories are optional so the
// pure loop-result evaluator remains useful in unit tests and batch jobs.
type Option func(*Service)

func WithRepository(repo port.Repository) Option {
	return func(s *Service) {
		s.agentRuns = repo.AgentRunRepo()
		s.evidence = repo.EvidenceRepo()
		s.votes = repo.VoteRepo()
		s.resolution = repo.ResolutionRepo()
		s.toolCalls = repo.ToolCallRepo()
	}
}

// NewService creates an EvaluationService.
func NewService(opts ...Option) *Service {
	s := &Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Evaluate computes evaluation metrics from loop results.
func (s *Service) Evaluate(ctx context.Context, results []*runtime.LoopResult, consensusRound int, consensusOutcome entity.ConsensusOutcome) (*entity.Evaluation, error) {
	return domainservice.Evaluate(results, consensusRound, consensusOutcome), nil
}

// EvaluateCase evaluates a completed case from its persisted run artifacts.
func (s *Service) EvaluateCase(ctx context.Context, caseID string) (*entity.Evaluation, error) {
	return s.evaluatePersistedCase(ctx, caseID)
}
func (s *Service) evaluatePersistedCase(ctx context.Context, caseID string) (*entity.Evaluation, error) {
	if caseID == "" {
		return nil, fmt.Errorf("evaluation: case ID is required")
	}
	if s.agentRuns == nil || s.evidence == nil || s.resolution == nil {
		return nil, fmt.Errorf("evaluation: repositories are not configured")
	}
	runs, err := s.agentRuns.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("evaluation: list agent runs: %w", err)
	}
	ev := &entity.Evaluation{}
	var totalReliability float64
	sourceTypes := make(map[string]bool)
	for _, run := range runs {
		if run == nil {
			continue
		}
		if run.Status == entity.AgentRunStatusMaxSteps {
			ev.MaxStepsExceeded++
		}
		if run.Usage != nil {
			ev.TotalTokens += run.Usage.TotalTokens
		}
	}
	if len(runs) > 0 {
		ev.AvgTokensPerAgent = float64(ev.TotalTokens) / float64(len(runs))
	}
	evidence, err := s.evidence.ListByCase(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("evaluation: list evidence: %w", err)
	}
	for _, item := range evidence {
		if item == nil {
			continue
		}
		ev.EvidenceCount++
		totalReliability += item.Reliability.Final
		sourceTypes[string(item.SourceType)] = true
	}
	if ev.EvidenceCount > 0 {
		ev.AvgReliability = totalReliability / float64(ev.EvidenceCount)
	}
	ev.UniqueSourceTypes = len(sourceTypes)
	totalCalls, successfulCalls := 0, 0
	if s.toolCalls != nil {
		calls, callErr := s.toolCalls.ListByCase(ctx, caseID)
		if callErr != nil {
			return nil, fmt.Errorf("evaluation: list tool calls: %w", callErr)
		}
		for _, call := range calls {
			if call == nil {
				continue
			}
			totalCalls++
			if call.Valid && call.Err == "" {
				successfulCalls++
			} else if !call.Valid {
				ev.ToolParamFailures++
			}
		}
	}
	ev.TotalToolCalls = totalCalls
	if totalCalls > 0 {
		ev.ToolSuccessRate = float64(successfulCalls) / float64(totalCalls)
		if len(runs) > 0 {
			ev.AvgToolCalls = float64(totalCalls) / float64(len(runs))
		}
	}
	resolution, err := s.resolution.Get(ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("evaluation: get resolution: %w", err)
	}
	if resolution != nil {
		ev.ConsensusRound = resolution.Consensus.Round
		ev.ConsensusOutcome = resolution.Consensus.Outcome
	}
	ev.FirstRoundConsensus = ev.ConsensusRound == 1 &&
		(ev.ConsensusOutcome == entity.ConsensusStrongApproval || ev.ConsensusOutcome == entity.ConsensusStrongRejection)
	return ev, nil
}

// Compare compares evaluations of two cases (future: fetches both and diffs).
func (s *Service) Compare(ctx context.Context, caseA, caseB string) (map[string]*entity.Evaluation, error) {
	evA, _ := s.EvaluateCase(ctx, caseA)
	evB, _ := s.EvaluateCase(ctx, caseB)
	return map[string]*entity.Evaluation{"a": evA, "b": evB}, nil
}

// Benchmark evaluates a batch and returns the first diagnostic error.
func (s *Service) Benchmark(ctx context.Context, caseIDs []string) (map[string]*entity.Evaluation, error) {
	if len(caseIDs) == 0 {
		return nil, fmt.Errorf("evaluation: at least one case ID is required")
	}
	result := make(map[string]*entity.Evaluation, len(caseIDs))
	for _, id := range caseIDs {
		if id == "" {
			return nil, fmt.Errorf("evaluation: case ID cannot be empty")
		}
		ev, err := s.EvaluateCase(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("evaluation: case %s: %w", id, err)
		}
		result[id] = ev
	}
	return result, nil
}
