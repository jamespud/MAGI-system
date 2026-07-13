package service

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type Evaluation struct {
	ToolSuccessRate    float64
	AvgToolCalls       float64
	ToolParamFailures  int
	EvidenceCount      int
	AvgReliability     float64
	UniqueSourceTypes  int
	GateFailures       int
	MaxStepsExceeded   int
	ValidationFailures int
	FirstRoundConsensus     bool
	ConsensusRound          int
	ConsensusOutcome        entity.ConsensusOutcome
	TotalTokens             int64
	AvgTokensPerAgent       float64
	TotalSteps              int
	TotalToolCalls          int
	CounterfactualStability float64
}

func Evaluate(results []*runtime.LoopResult, consensusRound int, consensusOutcome entity.ConsensusOutcome) *Evaluation {
	ev := &Evaluation{
		ConsensusRound:   consensusRound,
		ConsensusOutcome: consensusOutcome,
	}
	totalCalls, successCalls, paramFailCalls := 0, 0, 0
	var totalRel float64
	sourceTypes := make(map[string]bool)

	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Status == runtime.LoopStatusGateFailed {
			ev.GateFailures++
		}
		if r.Status == runtime.LoopStatusMaxSteps {
			ev.MaxStepsExceeded++
		}
		if r.Status == runtime.LoopStatusValidationFailed {
			ev.ValidationFailures++
		}
		if r.Usage != nil {
			ev.TotalTokens += r.Usage.TotalTokens
		}
		if r.Trace == nil {
			continue
		}
		for _, step := range r.Trace.Steps {
			ev.TotalSteps++
			for _, tc := range step.ToolCalls {
				totalCalls++
				if tc.Valid && tc.Err == "" {
					successCalls++
				} else if !tc.Valid {
					paramFailCalls++
				}
			}
		}
		if r.Ledger != nil {
			evs := r.Ledger.List()
			ev.EvidenceCount += len(evs)
			for _, e := range evs {
				totalRel += e.Reliability.Final
				sourceTypes[string(e.SourceType)] = true
			}
		}
	}

	n := len(results)
	if totalCalls > 0 {
		ev.ToolSuccessRate = float64(successCalls) / float64(totalCalls)
		ev.AvgToolCalls = float64(totalCalls) / float64(n)
		ev.ToolParamFailures = paramFailCalls
	}
	ev.TotalToolCalls = totalCalls
	if ev.EvidenceCount > 0 {
		ev.AvgReliability = totalRel / float64(ev.EvidenceCount)
	}
	ev.UniqueSourceTypes = len(sourceTypes)
	if n > 0 && ev.TotalTokens > 0 {
		ev.AvgTokensPerAgent = float64(ev.TotalTokens) / float64(n)
	}
	ev.FirstRoundConsensus = consensusRound == 1 &&
		(consensusOutcome == entity.ConsensusStrongApproval || consensusOutcome == entity.ConsensusStrongRejection)
	return ev
}

// CaseOrchestrator is the interface for re-running a case (avoids circular import).
type CaseOrchestrator interface {
	Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error)
}

// CounterfactualStability re-runs a case N times and returns the agreement rate
// of the final decision (ADR-010). N=3-5 recommended; cost = N x full case.
func CounterfactualStability(ctx context.Context, case_ *entity.DecisionCase, orch CaseOrchestrator, N int) float64 {
	if N <= 0 || orch == nil {
		return 0
	}
	counts := make(map[entity.VoteDecision]int)
	for i := 0; i < N; i++ {
		res, err := orch.Orchestrate(ctx, case_)
		if err != nil || res == nil {
			counts[entity.VoteDecisionAbstain]++
			continue
		}
		counts[res.FinalDecision]++
	}
	maxCount := 0
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}
	return float64(maxCount) / float64(N)
}
