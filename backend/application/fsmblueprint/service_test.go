package fsmblueprint_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/fsmblueprint"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type memBlueprintRepo struct {
	blueprint *entity.FSMBlueprint
}

func (m *memBlueprintRepo) Get(ctx context.Context) (*entity.FSMBlueprint, error) {
	return m.blueprint, nil
}
func (m *memBlueprintRepo) Save(ctx context.Context, b entity.FSMBlueprint) error {
	m.blueprint = &b
	return nil
}

func TestService_GetFallsBackToDefault(t *testing.T) {
	svc := fsmblueprint.NewService(&memBlueprintRepo{})
	b, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(b.Transitions) == 0 {
		t.Fatal("default blueprint must contain transitions")
	}
}

func TestService_SaveAndValidate(t *testing.T) {
	repo := &memBlueprintRepo{}
	svc := fsmblueprint.NewService(repo)
	ctx := context.Background()
	blueprint := entity.FSMBlueprint{Transitions: []entity.StateTransition{
		{From: "DRAFT", To: "NORMALIZING"},
	}}
	saved, err := svc.Save(ctx, blueprint)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(saved.Transitions) != 1 {
		t.Fatalf("saved = %+v", saved)
	}
	if saved.Transitions[0].Action != "normalize" {
		t.Fatalf("missing action must be filled from defaults, got %q", saved.Transitions[0].Action)
	}
	if _, err := svc.Save(ctx, entity.FSMBlueprint{}); err == nil {
		t.Fatal("empty blueprint must be rejected")
	}
	if _, err := svc.Save(ctx, entity.FSMBlueprint{Transitions: []entity.StateTransition{
		{From: "DRAFT", To: "NORMALIZING", Action: "not_a_real_action"},
	}}); err == nil {
		t.Fatal("unknown action must be rejected")
	}
	violations, err := svc.ValidatePath(ctx, []string{"DRAFT", "NORMALIZING", "RESOLVED"})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("violations = %v", violations)
	}
	reset, err := svc.Reset(ctx)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if len(reset.Transitions) == 0 {
		t.Fatal("reset must restore defaults")
	}
}

var _ port.FSMBlueprintRepository = (*memBlueprintRepo)(nil)
