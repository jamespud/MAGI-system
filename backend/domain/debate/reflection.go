package debate

import (
	"errors"
	"fmt"

	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/entity"
)

// ValidateReflection checks the "four-of-one" rule (ADR-009): a position change
// must cite new EV-ID(s), accept a previously-rejected claim, reject a
// previously-held claim. All referenced IDs must exist. Maintaining or
// strengthening does not require new evidence.
func ValidateReflection(
	r *entity.Reflection,
	prevVote *entity.Vote,
	ledger *evidence.EvidenceLedger,
	claimIDs map[string]bool,
	requireNewEvidence bool,
) error {
	if r == nil {
		return errors.New("nil reflection")
	}
	if prevVote == nil {
		return errors.New("nil previous vote")
	}

	switch r.PositionChange {
	case entity.PositionChangeMaintain, entity.PositionChangeStrengthen, entity.PositionChangeAbstain:
		if requireNewEvidence && len(r.NewEvidenceIDs) == 0 {
			return errors.New("requireNewEvidence policy: maintain/strengthen/abstain still requires new EV-ID citation")
		}
		return nil

	case entity.PositionChangeChange, entity.PositionChangeWeaken:
		// Must satisfy at least one of the four criteria.
		hasNew := len(r.NewEvidenceIDs) > 0
		hasAccept := len(r.AcceptedClaims) > 0
		hasReject := len(r.RejectedClaims) > 0
		if !hasNew && !hasAccept && !hasReject {
			return errors.New("position change requires new evidence, accepted claim, or rejected claim")
		}
		// Verify all referenced EV-IDs exist in the ledger.
		for _, evID := range r.NewEvidenceIDs {
			if !ledger.ExistsCollected(evID, "") {
				return fmt.Errorf("new evidence EV-ID %q not found in ledger", evID)
			}
		}
		// Verify all referenced Claim-IDs exist.
		for _, cID := range r.AcceptedClaims {
			if !claimIDs[cID] {
				return fmt.Errorf("accepted claim %q not found", cID)
			}
		}
		for _, cID := range r.RejectedClaims {
			if !claimIDs[cID] {
				return fmt.Errorf("rejected claim %q not found", cID)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown position change: %q", r.PositionChange)
	}
}

// InferReflection constructs a Reflection by diffing prevVote and newVote.
// PositionChange: decision differs -> change; same -> maintain.
// NewEvidenceIDs: newVote.EvidenceIDs minus prevVote.EvidenceIDs.
// AcceptedClaims/RejectedClaims: nil (cannot infer from vote diff; S4+ may add).
func InferReflection(
	prevVote *entity.Vote,
	newVote *entity.Vote,
	round int,
) *entity.Reflection {
	if prevVote == nil || newVote == nil {
		return nil
	}
	pos := entity.PositionChangeMaintain
	if prevVote.Decision != newVote.Decision {
		pos = entity.PositionChangeChange
	}
	prevEV := make(map[string]bool, len(prevVote.EvidenceIDs))
	for _, id := range prevVote.EvidenceIDs {
		prevEV[id] = true
	}
	var newEV []string
	for _, id := range newVote.EvidenceIDs {
		if !prevEV[id] {
			newEV = append(newEV, id)
		}
	}
	return &entity.Reflection{
		AgentRunID:     newVote.AgentRunID,
		Round:          round,
		PreviousVoteID: prevVote.ID,
		PositionChange: pos,
		NewEvidenceIDs: newEV,
		Reasoning:      "inferred from vote diff",
		ReadyToRevote:  true,
	}
}
