package runtime

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
)

// CheckpointService is a small, explicit wrapper around
// port.CheckpointRepository that owns durable checkpoint persistence and its
// durable execution-history events. It fails closed: a configured repository
// Load or Save error stops the caller before a following model/tool invocation,
// while a nil repository stays stateless.
type CheckpointService struct {
	repo     port.CheckpointRepository
	recorder execution.EventRecorder
}

func NewCheckpointService(repo port.CheckpointRepository, recorder execution.EventRecorder) *CheckpointService {
	return &CheckpointService{repo: repo, recorder: recorder}
}

// Load returns the persisted agent state, or nil for a missing checkpoint or an
// unconfigured repository. A configured repository error is returned so the
// caller can stop before any invocation.
func (s *CheckpointService) Load(ctx context.Context, runID string) (*entity.AgentState, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	return s.repo.Load(ctx, runID)
}

// Commit persists a checkpoint snapshot and records a durable execution-history
// event. A configured repository Save error or a CHECKPOINT_COMMITTED record
// failure stops the caller (fail closed). A nil repository is stateless.
func (s *CheckpointService) Commit(ctx context.Context, caseID, runID string, agentCode entity.MagiCode, state *entity.AgentState) error {
	if s == nil || s.repo == nil || state == nil {
		return nil
	}
	ac := agentCode
	if err := s.repo.Save(ctx, state); err != nil {
		if s.recorder != nil {
			_ = s.recorder.Critical(ctx, entity.NewEvent(caseID, runID, &ac, entity.EventCheckpointFailed, map[string]any{"step": state.StepCount + 1, "error": err.Error()}))
		}
		return err
	}
	if s.recorder == nil {
		return nil
	}
	return s.recorder.Critical(ctx, entity.NewEvent(caseID, runID, &ac, entity.EventCheckpointCommitted, map[string]any{"step": state.StepCount + 1}))
}
