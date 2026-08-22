package memory

import (
	"fmt"
	"strings"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
)

// BuildProjection creates a CaseMemoryProjection from a resolved case for
// long-term RAG storage (ADR-006). Summaries are truncated (no LLM call).
func BuildProjection(
	case_ *entity.DecisionCase,
	resolution *entity.Resolution,
	ledger *evidence.EvidenceLedger,
	votes []*entity.Vote,
	remap *entity.ArtifactRemap,
) *entity.CaseMemoryProjection {
	proj := &entity.CaseMemoryProjection{ProjectionVersion: 1}

	if case_ != nil {
		proj.CaseID = case_.ID
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
			persisted := remapID(remap, ev.ID)
			if remap != nil && persisted == ev.ID {
				// Merged ledgers re-number evidence (EV-001, EV-002, ...); the
				// remap only knows the per-agent original IDs, so entries that
				// were never persisted must not leak into the projection.
				continue
			}
			proj.KeyEvidence = append(proj.KeyEvidence, entity.MemoryEvidence{
				EvidenceID:  persisted,
				Observation: ev.Observation,
				Reliability: ev.Reliability.Final,
			})
		}
		cls := ledger.ListClaims()
		for i, cl := range cls {
			if i >= 5 {
				break
			}
			persisted := remapID(remap, cl.ID)
			if remap != nil && persisted == cl.ID {
				continue
			}
			proj.KeyClaims = append(proj.KeyClaims, entity.MemoryClaim{
				ClaimID:   persisted,
				Statement: cl.Statement,
			})
		}
	}

	for _, v := range votes {
		proj.Votes = append(proj.Votes, entity.MemoryVote{
			MagiCode:   entity.CodeOfRun(v.AgentRunID),
			Decision:   v.Decision,
			Confidence: v.Confidence,
		})
	}

	proj.IndexDoc = renderIndexDoc(proj, case_, resolution)
	return proj
}

// renderIndexDoc renders the full long-text index document: full background
// and full resolution (not truncated) plus the projection's evidence/claims/
// votes. It mirrors RenderDocument but is decoupled from the truncated display
// fields so a case can fill the 900/1800 hierarchy levels.
func renderIndexDoc(proj *entity.CaseMemoryProjection, case_ *entity.DecisionCase, resolution *entity.Resolution) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", proj.QuestionSummary)
	ctx := ""
	if case_ != nil {
		ctx = case_.Context
	}
	fmt.Fprintf(&b, "Context: %s\n", ctx)
	if len(proj.KeyEvidence) > 0 {
		b.WriteString("Evidence:\n")
		for _, ev := range proj.KeyEvidence {
			fmt.Fprintf(&b, "- [%s] (reliability %.2f) %s\n", ev.EvidenceID, ev.Reliability, ev.Observation)
		}
	}
	if len(proj.KeyClaims) > 0 {
		b.WriteString("Claims:\n")
		for _, cl := range proj.KeyClaims {
			fmt.Fprintf(&b, "- [%s] %s\n", cl.ClaimID, cl.Statement)
		}
	}
	if len(proj.Votes) > 0 {
		b.WriteString("Votes:\n")
		for _, v := range proj.Votes {
			fmt.Fprintf(&b, "- %s: %s (confidence %.2f)\n", v.MagiCode, v.Decision, v.Confidence)
		}
	}
	if len(proj.Tags) > 0 {
		b.WriteString("Tags:\n")
		for _, tag := range proj.Tags {
			fmt.Fprintf(&b, "- %s\n", tag)
		}
	}
	if proj.Annotation != "" {
		fmt.Fprintf(&b, "Annotation: %s\n", proj.Annotation)
	}
	res := ""
	if resolution != nil {
		res = resolution.FinalReport
	}
	if res != "" {
		fmt.Fprintf(&b, "Resolution: %s\n", res)
	}
	if proj.Outcome != nil {
		fmt.Fprintf(&b, "Outcome: status=%s learned=%s\n", proj.Outcome.Status, proj.Outcome.Learned)
	}
	return b.String()
}

func remapID(remap *entity.ArtifactRemap, id string) string {
	if remap == nil {
		return id
	}
	out := remap.RemapList([]string{id})
	if len(out) == 1 {
		return out[0]
	}
	return id
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
