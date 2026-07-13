package memory

import (
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/entity"
)

// BuildProjection creates a CaseMemoryProjection from a resolved case for
// long-term RAG storage (ADR-006). Summaries are truncated (no LLM call).
func BuildProjection(
	case_ *entity.DecisionCase,
	resolution *entity.Resolution,
	ledger *evidence.EvidenceLedger,
	votes []*entity.Vote,
) *entity.CaseMemoryProjection {
	proj := &entity.CaseMemoryProjection{ProjectionVersion: 1}

	if case_ != nil {
		proj.QuestionSummary = case_.Question
		proj.ContextSummary = truncateRunes(case_.Context, 200)
	}

	if resolution != nil {
		if resolution.FinalReport != "" {
			proj.ContextSummary = truncateRunes(resolution.FinalReport, 200)
			proj.Resolution = truncateRunes(resolution.FinalReport, 500)
		}
		proj.Outcome = &entity.CaseOutcome{Status: string(resolution.Consensus.Outcome)}
	}

	if ledger != nil {
		evs := ledger.List()
		for i, ev := range evs {
			if i >= 5 {
				break
			}
			proj.KeyEvidence = append(proj.KeyEvidence, entity.MemoryEvidence{
				EvidenceID:  ev.ID,
				Observation: ev.Observation,
				Reliability: ev.Reliability.Final,
			})
		}
		cls := ledger.ListClaims()
		for i, cl := range cls {
			if i >= 5 {
				break
			}
			proj.KeyClaims = append(proj.KeyClaims, entity.MemoryClaim{
				ClaimID:   cl.ID,
				Statement: cl.Statement,
			})
		}
	}

	for _, v := range votes {
		proj.Votes = append(proj.Votes, entity.MemoryVote{
			MagiCode:   entity.MagiCode(voteMagiCode(v)),
			Decision:   v.Decision,
			Confidence: v.Confidence,
		})
	}

	return proj
}

func voteMagiCode(v *entity.Vote) string {
	// AgentRunID doesn't carry MagiCode in S4; S5 Orchestrator can set it.
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
