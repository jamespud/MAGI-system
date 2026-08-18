package entity_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestDefaultFSMBlueprint_ValidatesPath(t *testing.T) {
	blueprint := entity.DefaultFSMBlueprint()
	valid := []string{
		string(entity.CaseStatusDraft),
		string(entity.CaseStatusNormalizing),
		string(entity.CaseStatusContextBuilding),
		string(entity.CaseStatusRetrievingMemory),
		string(entity.CaseStatusInvestigating),
		string(entity.CaseStatusEvidenceGating),
		string(entity.CaseStatusCollectingVotes),
		string(entity.CaseStatusConsensusCheck),
		string(entity.CaseStatusDebating),
		string(entity.CaseStatusReflecting),
		string(entity.CaseStatusRevoting),
		string(entity.CaseStatusConsensusCheck),
		string(entity.CaseStatusResolving),
		string(entity.CaseStatusGeneratingReport),
		string(entity.CaseStatusSavingMemory),
		string(entity.CaseStatusEvaluating),
		string(entity.CaseStatusResolved),
	}
	if violations := blueprint.ValidatePath(valid); len(violations) != 0 {
		t.Fatalf("valid path rejected: %v", violations)
	}

	invalid := []string{
		string(entity.CaseStatusDraft),
		string(entity.CaseStatusResolved), // direct jump is not allowed
	}
	if violations := blueprint.ValidatePath(invalid); len(violations) != 1 {
		t.Fatalf("invalid path must produce one violation, got %v", violations)
	}

	if violations := blueprint.ValidatePath([]string{string(entity.CaseStatusDraft), string(entity.CaseStatusDraft)}); len(violations) != 0 {
		t.Fatalf("self-loop should be allowed, got %v", violations)
	}
}
