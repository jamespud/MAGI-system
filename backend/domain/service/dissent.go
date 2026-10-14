package service

import "github.com/jamespud/magi/backend/domain/entity"

// ExtractDissent returns the structured minority positions for a final
// decision. codes is index-aligned with votes: a caller that holds the ballots
// next to the agent configs (the orchestrator does) passes the code that
// produced each ballot, so attribution never depends on a persisted run id or
// on a join that a legacy row may lack. An abstention is not dissent, and a
// missing ballot slot (nil) is skipped rather than fabricated.
//
// The nil-ness of the result is part of the contract: a complete round with no
// dissent returns a non-nil empty slice ("recorded none"), while an
// undecidable round (no final decision or no ballots) returns nil ("not
// recorded"). Callers persist the distinction so a legacy "no record" is never
// presented as "no dissent".
func ExtractDissent(final entity.VoteDecision, votes []*entity.Vote, codes []entity.MagiCode) []entity.Dissent {
	if final == "" || len(votes) == 0 {
		return nil
	}
	out := []entity.Dissent{}
	for i, v := range votes {
		if v == nil || v.Decision == final || v.Decision == entity.VoteDecisionAbstain {
			continue
		}
		var code entity.MagiCode
		if i < len(codes) {
			code = codes[i]
		}
		out = append(out, entity.Dissent{
			AgentCode:   code,
			Decision:    v.Decision,
			Reasoning:   v.ReasoningSummary,
			EvidenceIDs: v.EvidenceIDs,
			ClaimIDs:    v.KeyClaimIDs,
			Conditions:  v.Conditions,
		})
	}
	return out
}
