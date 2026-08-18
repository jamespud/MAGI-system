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

func TestDefaultFSMBlueprint_ActionsPopulated(t *testing.T) {
	blueprint := entity.DefaultFSMBlueprint()
	if len(blueprint.Transitions) == 0 {
		t.Fatal("default blueprint must contain transitions")
	}
	for _, tr := range blueprint.Transitions {
		if tr.Action == "" {
			t.Fatalf("transition %s -> %s has no action", tr.From, tr.To)
		}
		if tr.Action != entity.ActionForStatus(tr.To) {
			t.Fatalf("transition %s -> %s action = %q, want %q", tr.From, tr.To, tr.Action, entity.ActionForStatus(tr.To))
		}
		if got := blueprint.ActionFor(tr.From, tr.To); got != tr.Action {
			t.Fatalf("ActionFor(%s,%s) = %q, want %q", tr.From, tr.To, got, tr.Action)
		}
	}
	if got := blueprint.ActionFor("NOPE", "NOPE"); got != "" {
		t.Fatalf("ActionFor unknown transition = %q, want empty", got)
	}
}

func TestFSMBlueprint_UnknownActions(t *testing.T) {
	unknown := entity.FSMBlueprint{Transitions: []entity.StateTransition{
		{From: "A", To: "B", Action: "not_a_real_action"},
	}}
	if violations := unknown.UnknownActions(); len(violations) != 1 {
		t.Fatalf("unknown actions = %v, want 1", violations)
	}
	// Empty actions are tolerated for legacy blueprints.
	legacy := entity.FSMBlueprint{Transitions: []entity.StateTransition{
		{From: "A", To: "B"},
	}}
	if violations := legacy.UnknownActions(); len(violations) != 0 {
		t.Fatalf("legacy empty actions = %v, want none", violations)
	}
}
