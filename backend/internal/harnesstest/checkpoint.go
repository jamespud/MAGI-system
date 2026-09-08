package harnesstest

import (
	"context"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// FailingCheckpointRepository is an in-memory port.CheckpointRepository that can
// be configured to fail Save or Load and records save/load counts.
type FailingCheckpointRepository struct {
	mu sync.Mutex

	state   *entity.AgentState
	saveErr error
	loadErr error
	Saves   int
	Loads   int
}

func NewFailingCheckpointRepository(state *entity.AgentState) *FailingCheckpointRepository {
	return &FailingCheckpointRepository{state: state}
}

func (r *FailingCheckpointRepository) SetSaveError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saveErr = err
}

func (r *FailingCheckpointRepository) SetLoadError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadErr = err
}

func (r *FailingCheckpointRepository) Save(_ context.Context, state *entity.AgentState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Saves++
	if r.saveErr != nil {
		return r.saveErr
	}
	r.state = state
	return nil
}

func (r *FailingCheckpointRepository) Load(_ context.Context, _ string) (*entity.AgentState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Loads++
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	if r.state == nil {
		return nil, nil
	}
	cp := *r.state
	return &cp, nil
}

func (r *FailingCheckpointRepository) State() *entity.AgentState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == nil {
		return nil
	}
	cp := *r.state
	return &cp
}

var _ port.CheckpointRepository = (*FailingCheckpointRepository)(nil)
