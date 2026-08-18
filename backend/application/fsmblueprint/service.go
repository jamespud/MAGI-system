package fsmblueprint

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages the editable orchestration blueprint and validates case
// status histories against it.
type Service struct {
	repo port.FSMBlueprintRepository
}

func NewService(repo port.FSMBlueprintRepository) *Service {
	return &Service{repo: repo}
}

// Get returns the stored blueprint or the built-in FSM transitions.
func (s *Service) Get(ctx context.Context) (entity.FSMBlueprint, error) {
	stored, err := s.repo.Get(ctx)
	if err != nil {
		return entity.FSMBlueprint{}, err
	}
	if stored != nil {
		return *stored, nil
	}
	return entity.DefaultFSMBlueprint(), nil
}

// Save persists the blueprint.
func (s *Service) Save(ctx context.Context, b entity.FSMBlueprint) (entity.FSMBlueprint, error) {
	if len(b.Transitions) == 0 {
		return entity.FSMBlueprint{}, fmt.Errorf("fsm blueprint: at least one transition is required")
	}
	if err := s.repo.Save(ctx, b); err != nil {
		return entity.FSMBlueprint{}, err
	}
	return b, nil
}

// Reset restores the built-in FSM transitions.
func (s *Service) Reset(ctx context.Context) (entity.FSMBlueprint, error) {
	def := entity.DefaultFSMBlueprint()
	if err := s.repo.Save(ctx, def); err != nil {
		return entity.FSMBlueprint{}, err
	}
	return def, nil
}

// ValidatePath checks a case status history against the stored blueprint.
func (s *Service) ValidatePath(ctx context.Context, path []string) ([]string, error) {
	blueprint, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	return blueprint.ValidatePath(path), nil
}
