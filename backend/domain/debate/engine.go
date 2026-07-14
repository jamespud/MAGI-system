package debate

import (
	"github.com/jamespud/magi/backend/domain/claim"
	"github.com/jamespud/magi/backend/domain/entity"
)

// DebateEngine constructs DebatePackets for the reconsideration phase.
// It is a deterministic packet builder; it does NOT call LLMs.
type DebateEngine struct {
	finder claim.ConflictFinder
}

func NewDebateEngine(finder claim.ConflictFinder) *DebateEngine {
	if finder == nil {
		finder = claim.NewNilConflictFinder()
	}
	return &DebateEngine{finder: finder}
}

// BuildPacket groups votes into majority/minority by Decision, extracts
// conflicting claims via the ConflictFinder, and assembles a DebatePacket.
func (e *DebateEngine) BuildPacket(
	votes []entity.Vote,
	claims []*entity.Claim,
	round int,
	sharedEvidence []*entity.EvidenceRecord,
) entity.DebatePacket {
	var approves, rejects, others []entity.Vote
	for _, v := range votes {
		switch v.Decision {
		case entity.VoteDecisionApprove, entity.VoteDecisionConditionalApprove:
			approves = append(approves, v)
		case entity.VoteDecisionReject:
			rejects = append(rejects, v)
		default:
			others = append(others, v)
		}
	}

	var majority, minority []entity.Vote
	if len(approves) > len(rejects) && len(approves) > 0 {
		majority = approves
		minority = append(minority, rejects...)
		minority = append(minority, others...)
	} else if len(rejects) > len(approves) && len(rejects) > 0 {
		majority = rejects
		minority = append(minority, approves...)
		minority = append(minority, others...)
	} else {
		// all abstain or empty -> all minority
		minority = append(minority, approves...)
		minority = append(minority, rejects...)
		minority = append(minority, others...)
	}

	conflicts := e.finder.Find(claims)
	if len(conflicts) == 0 && len(majority) > 0 && len(minority) > 0 {
		conflicts = synthesizeConflicts(majority, minority)
	}

	shared := make([]entity.EvidenceRecord, len(sharedEvidence))
	for i, e := range sharedEvidence {
		if e != nil {
			shared[i] = *e
		}
	}

	return entity.DebatePacket{
		Round:             round,
		MajorityVotes:     majority,
		MinorityVotes:     minority,
		ConflictingClaims: conflicts,
		SharedEvidence:    shared,
	}
}

// synthesizeConflicts builds deterministic ClaimConflict pairs from the vote
// split when agents assert no explicit contradictions. It pairs majority
// KeyClaimIDs with minority KeyClaimIDs by index, capped at 3 pairs, so the
// debate has concrete claims to target (design §16).
func synthesizeConflicts(majority, minority []entity.Vote) []entity.ClaimConflict {
	var out []entity.ClaimConflict
	for _, maj := range majority {
		for _, min := range minority {
			for i := 0; i < len(maj.KeyClaimIDs) && i < len(min.KeyClaimIDs); i++ {
				out = append(out, entity.ClaimConflict{
					ClaimA: maj.KeyClaimIDs[i],
					ClaimB: min.KeyClaimIDs[i],
					Reason: "opposing-vote",
				})
				if len(out) >= 3 {
					return out
				}
			}
			if len(out) >= 3 {
				return out
			}
		}
	}
	return out
}
