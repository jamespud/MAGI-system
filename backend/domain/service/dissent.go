package service

import "github.com/jamespud/magi/backend/domain/entity"

// ExtractDissent returns the structured minority positions for a final
// decision. Abstentions are not dissent; a deadlock/insufficient-quorum has no
// majority to dissent from, so an empty list is returned.
func ExtractDissent(final entity.VoteDecision, votes []*entity.Vote, codeByRun map[string]entity.MagiCode) []entity.Dissent {
	if final == "" || len(votes) == 0 {
		return nil
	}
	var out []entity.Dissent
	for _, v := range votes {
		if v == nil || v.Decision == final || v.Decision == entity.VoteDecisionAbstain {
			continue
		}
		out = append(out, entity.Dissent{
			AgentCode:   codeByRun[v.AgentRunID],
			Decision:    v.Decision,
			Reasoning:   v.ReasoningSummary,
			EvidenceIDs: v.EvidenceIDs,
			ClaimIDs:    v.KeyClaimIDs,
			Conditions:  v.Conditions,
		})
	}
	return out
}
