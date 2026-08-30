package magi

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

var errRuntimeInvocationFenceLost = errors.New("runtime invocation fencing ownership lost")

type runtimeInvocationRepo struct {
	db *gorm.DB
}

func NewRuntimeInvocationRepository(db *gorm.DB) port.RuntimeInvocationRepository {
	return &runtimeInvocationRepo{db: db}
}

func (r *runtimeInvocationRepo) Ensure(ctx context.Context, invocation *entity.RuntimeInvocation) (*entity.RuntimeInvocation, error) {
	if invocation == nil {
		return nil, errors.New("runtime invocation is required")
	}
	model := RuntimeInvocationModel{
		InvocationID: invocation.InvocationID, RunID: invocation.RunID, StepID: invocation.StepID,
		Kind: invocation.Kind, LogicalOrdinal: invocation.LogicalOrdinal,
		Status: string(invocation.Status), AttemptCount: invocation.AttemptCount,
		OperationName: invocation.OperationName, RetrySafety: invocation.RetrySafety,
		IdempotencyKey: invocation.IdempotencyKey,
		InputDigest:    invocation.InputDigest, InputJSON: invocation.InputJSON,
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model).Error; err != nil {
		return nil, err
	}
	return r.Get(ctx, invocation.InvocationID)
}

func (r *runtimeInvocationRepo) Get(ctx context.Context, invocationID string) (*entity.RuntimeInvocation, error) {
	var model RuntimeInvocationModel
	if err := r.db.WithContext(ctx).First(&model, "invocation_id = ?", invocationID).Error; err != nil {
		return nil, err
	}
	return runtimeInvocationFromModel(&model), nil
}

func (r *runtimeInvocationRepo) BeginAttempt(ctx context.Context, invocationID, attemptID string) (*entity.RuntimeInvocation, bool, error) {
	var invocation RuntimeInvocationModel
	won := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := tx.NowFunc()
		result := tx.Model(&RuntimeInvocationModel{}).
			Where("invocation_id = ? AND status IN ?", invocationID, []string{
				string(entity.InvocationPending),
				string(entity.InvocationFailed),
				string(entity.InvocationUnknown),
			}).
			Updates(map[string]any{
				"status":        string(entity.InvocationRunning),
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"error":         nil,
				"started_at":    now,
				"completed_at":  nil,
				"updated_at":    now,
			})
		if result.Error != nil {
			return result.Error
		}
		if err := tx.First(&invocation, "invocation_id = ?", invocationID).Error; err != nil {
			return err
		}
		if result.RowsAffected == 0 {
			return nil
		}

		attempt := RuntimeInvocationAttemptModel{
			AttemptID: attemptID, InvocationID: invocationID, AttemptNo: invocation.AttemptCount,
			Status: string(entity.InvocationRunning), StartedAt: &now,
		}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}

		won = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return runtimeInvocationFromModel(&invocation), won, nil
}

func (r *runtimeInvocationRepo) Complete(ctx context.Context, invocationID, attemptID, outputJSON string) (bool, error) {
	return r.finishAttempt(ctx, invocationID, attemptID, entity.InvocationSucceeded, outputJSON, "")
}

func (r *runtimeInvocationRepo) Fail(ctx context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finishAttempt(ctx, invocationID, attemptID, entity.InvocationFailed, "", reason)
}

func (r *runtimeInvocationRepo) MarkUnknown(ctx context.Context, invocationID, attemptID, reason string) (bool, error) {
	return r.finishAttempt(ctx, invocationID, attemptID, entity.InvocationUnknown, "", reason)
}

func (r *runtimeInvocationRepo) finishAttempt(ctx context.Context, invocationID, attemptID string, status entity.InvocationStatus, outputJSON, reason string) (bool, error) {
	won := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := tx.NowFunc()
		attemptUpdates := map[string]any{
			"status":       string(status),
			"completed_at": now,
		}
		if status != entity.InvocationSucceeded {
			attemptUpdates["error"] = reason
		}
		attemptResult := tx.Model(&RuntimeInvocationAttemptModel{}).
			Where("attempt_id = ? AND invocation_id = ? AND status = ?", attemptID, invocationID, string(entity.InvocationRunning)).
			Updates(attemptUpdates)
		if attemptResult.Error != nil {
			return attemptResult.Error
		}
		if attemptResult.RowsAffected != 1 {
			return nil
		}

		invocationUpdates := map[string]any{
			"status":       string(status),
			"completed_at": now,
			"updated_at":   now,
		}
		if status == entity.InvocationSucceeded {
			invocationUpdates["output_json"] = outputJSON
			invocationUpdates["error"] = nil
		} else {
			invocationUpdates["error"] = reason
		}
		invocationResult := tx.Model(&RuntimeInvocationModel{}).
			Where("invocation_id = ? AND status = ?", invocationID, string(entity.InvocationRunning)).
			Updates(invocationUpdates)
		if invocationResult.Error != nil {
			return invocationResult.Error
		}
		if invocationResult.RowsAffected != 1 {
			return errRuntimeInvocationFenceLost
		}
		won = true
		return nil
	})
	if errors.Is(err, errRuntimeInvocationFenceLost) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return won, nil
}

func runtimeInvocationFromModel(model *RuntimeInvocationModel) *entity.RuntimeInvocation {
	if model == nil {
		return nil
	}
	invocation := &entity.RuntimeInvocation{
		InvocationID: model.InvocationID, RunID: model.RunID, StepID: model.StepID,
		Kind: model.Kind, LogicalOrdinal: model.LogicalOrdinal,
		Status: entity.InvocationStatus(model.Status), AttemptCount: model.AttemptCount,
		OperationName: model.OperationName, RetrySafety: model.RetrySafety,
		IdempotencyKey: model.IdempotencyKey,
		InputDigest:    model.InputDigest, InputJSON: model.InputJSON,
		StartedAt: model.StartedAt, CompletedAt: model.CompletedAt, UpdatedAt: model.UpdatedAt,
	}
	if model.OutputJSON != nil {
		invocation.OutputJSON = *model.OutputJSON
	}
	if model.Error != nil {
		invocation.Error = *model.Error
	}
	return invocation
}

var _ port.RuntimeInvocationRepository = (*runtimeInvocationRepo)(nil)
