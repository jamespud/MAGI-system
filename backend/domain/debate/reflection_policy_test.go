package debate_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/entity"
)

// S7b: RequireNewEvidence=true rejects maintain without new EV-IDs.
func TestValidateReflection_RequireNewEvidence_MaintainRejected(t *testing.T) {
	r := &entity.Reflection{PositionChange: entity.PositionChangeMaintain}
	err := debate.ValidateReflection(r, &entity.Vote{}, evidence.NewEvidenceLedger("c", "r", "m"), map[string]bool{}, true)
	if err == nil {
		t.Fatalf("expected error: maintain without new evidence should be rejected when requireNewEvidence=true")
	}
}

// S7b: RequireNewEvidence=true accepts maintain WITH new EV-IDs.
func TestValidateReflection_RequireNewEvidence_MaintainWithEvidence(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c", "r", "m")
	ledger.Record("tc", "tool", "local", "", "obs", entity.ReliabilityScore{Final: 0.9})
	evID := ledger.List()[0].ID
	r := &entity.Reflection{PositionChange: entity.PositionChangeMaintain, NewEvidenceIDs: []string{evID}}
	err := debate.ValidateReflection(r, &entity.Vote{}, ledger, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("expected valid: maintain with new evidence should pass when requireNewEvidence=true: %v", err)
	}
}

// S7b: RequireNewEvidence=false still allows maintain without evidence (backward compat).
func TestValidateReflection_RequireNewEvidence_Disabled(t *testing.T) {
	r := &entity.Reflection{PositionChange: entity.PositionChangeMaintain}
	err := debate.ValidateReflection(r, &entity.Vote{}, evidence.NewEvidenceLedger("c", "r", "m"), map[string]bool{}, false)
	if err != nil {
		t.Fatalf("expected valid: maintain without evidence should pass when requireNewEvidence=false: %v", err)
	}
}
