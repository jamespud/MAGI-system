package service

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type Evaluation struct {
	ToolSuccessRate         float64
	AvgToolCalls            float64
	GateFailures            int
	FirstRoundConsensus     bool
	TotalTokens             int64
	CounterfactualStability float64
}

func Evaluate(results []*runtime.LoopResult, consensusRound int, consensusOutcome entity.ConsensusOutcome) *Evaluation {
	ev := &Evaluation{}
	totalCalls, successCalls := 0, 0
	for _, r := range results {
		if r == nil {
			continue
		}
		if r.Usage != nil {
			ev.TotalTokens += r.Usage.TotalTokens
		}
		for _, step := range r.Trace.Steps {
			for _, tc := range step.ToolCalls {
				totalCalls++
				if tc.Valid {
					successCalls++
				}
			}
		}
	}
	if totalCalls > 0 {
		ev.ToolSuccessRate = float64(successCalls) / float64(totalCalls)
		ev.AvgToolCalls = float64(totalCalls) / float64(len(results))
	}
	ev.FirstRoundConsensus = consensusRound == 1 && (consensusOutcome == entity.ConsensusStrongApproval || consensusOutcome == entity.ConsensusStrongRejection)
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
