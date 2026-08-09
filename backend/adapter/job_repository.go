package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

const defaultJobAttempts = 3

type decisionJobRepo struct {
	db *gorm.DB
}

func NewDecisionJobRepository(db *gorm.DB) port.DecisionJobRepository {
	return &decisionJobRepo{db: db}
}

func (r *decisionJobRepo) Enqueue(ctx context.Context, caseID string, maxAttempts int) (*entity.DecisionJob, error) {
	if caseID == "" {
		return nil, fmt.Errorf("decision job: case ID is required")
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultJobAttempts
	}
	var model DecisionJobModel
	err := r.db.WithContext(ctx).Where("case_id = ?", caseID).First(&model).Error
	if err == nil {
		switch entity.DecisionJobStatus(model.Status) {
		case entity.DecisionJobSucceeded:
			return jobFromModel(&model), nil
		case entity.DecisionJobQueued, entity.DecisionJobRunning:
			return jobFromModel(&model), nil
		default:
			now := time.Now()
			updates := map[string]any{
				"status":       string(entity.DecisionJobQueued),
				"attempt":      0,
				"max_attempts": maxAttempts,
				"worker_id":    "",
				"lease_until":  nil,
				"available_at": now,
				"last_error":   "",
				"updated_at":   now,
			}
			if err := r.db.WithContext(ctx).Model(&DecisionJobModel{}).Where("id = ?", model.ID).Updates(updates).Error; err != nil {
				return nil, err
			}
			return r.get(ctx, model.ID)
		}
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	model = DecisionJobModel{
		ID: "job-" + uuid.NewString(), CaseID: caseID, Status: string(entity.DecisionJobQueued),
		MaxAttempts: maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		// A concurrent enqueue may have won the unique case_id race.
		if existing, getErr := r.getByCase(ctx, caseID); getErr == nil {
			return existing, nil
		}
		return nil, err
	}
	return jobFromModel(&model), nil
}

func (r *decisionJobRepo) Claim(ctx context.Context, jobID, workerID string, leaseUntil time.Time) (*entity.DecisionJob, bool, error) {
	var claimed DecisionJobModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND status = ? AND available_at <= ?", jobID, string(entity.DecisionJobQueued), time.Now()).First(&claimed).Error; err != nil {
			return err
		}
		result := tx.Model(&DecisionJobModel{}).
			Where("id = ? AND status = ?", jobID, string(entity.DecisionJobQueued)).
			Updates(map[string]any{
				"status": string(entity.DecisionJobRunning), "worker_id": workerID,
				"lease_until": leaseUntil, "attempt": claimed.Attempt + 1, "updated_at": time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		claimed.Status = string(entity.DecisionJobRunning)
		claimed.WorkerID = workerID
		claimed.LeaseUntil = &leaseUntil
		claimed.Attempt++
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return jobFromModel(&claimed), true, nil
}

func (r *decisionJobRepo) Heartbeat(ctx context.Context, jobID, workerID string, leaseUntil time.Time) error {
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.DecisionJobRunning), workerID).
		Updates(map[string]any{"lease_until": leaseUntil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: lease lost")
	}
	return nil
}

func (r *decisionJobRepo) MarkSucceeded(ctx context.Context, jobID, workerID string) error {
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.DecisionJobRunning), workerID).
		Updates(map[string]any{"status": string(entity.DecisionJobSucceeded), "worker_id": "", "lease_until": nil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: cannot mark succeeded")
	}
	return nil
}

func (r *decisionJobRepo) MarkFailed(ctx context.Context, jobID, workerID, lastError string, retryAt *time.Time) error {
	updates := map[string]any{"worker_id": "", "lease_until": nil, "last_error": lastError, "updated_at": time.Now()}
	if retryAt != nil {
		updates["status"] = string(entity.DecisionJobQueued)
		updates["available_at"] = *retryAt
	} else {
		updates["status"] = string(entity.DecisionJobFailed)
	}
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.DecisionJobRunning), workerID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: cannot mark failed")
	}
	return nil
}

func (r *decisionJobRepo) Cancel(ctx context.Context, jobID string) error {
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status IN ?", jobID,
			[]string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning)}).
		Updates(map[string]any{"status": string(entity.DecisionJobCancelled), "worker_id": "", "lease_until": nil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: cannot cancel")
	}
	return nil
}

func (r *decisionJobRepo) RequeueExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", string(entity.DecisionJobRunning), now).
		Updates(map[string]any{"status": string(entity.DecisionJobQueued), "worker_id": "", "lease_until": nil, "available_at": now, "updated_at": now}).Error
}

func (r *decisionJobRepo) ListRunnable(ctx context.Context, now time.Time) ([]*entity.DecisionJob, error) {
	var models []DecisionJobModel
	if err := r.db.WithContext(ctx).
		Where("status = ? AND available_at <= ?", string(entity.DecisionJobQueued), now).
		Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.DecisionJob, len(models))
	for i := range models {
		out[i] = jobFromModel(&models[i])
	}
	return out, nil
}

func (r *decisionJobRepo) GetByCase(ctx context.Context, caseID string) (*entity.DecisionJob, error) {
	return r.getByCase(ctx, caseID)
}

func (r *decisionJobRepo) get(ctx context.Context, id string) (*entity.DecisionJob, error) {
	var model DecisionJobModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return jobFromModel(&model), nil
}

func (r *decisionJobRepo) getByCase(ctx context.Context, caseID string) (*entity.DecisionJob, error) {
	var model DecisionJobModel
	if err := r.db.WithContext(ctx).First(&model, "case_id = ?", caseID).Error; err != nil {
		return nil, err
	}
	return jobFromModel(&model), nil
}

func jobFromModel(model *DecisionJobModel) *entity.DecisionJob {
	return &entity.DecisionJob{
		ID: model.ID, CaseID: model.CaseID, Status: entity.DecisionJobStatus(model.Status),
		Attempt: model.Attempt, MaxAttempts: model.MaxAttempts, WorkerID: model.WorkerID,
		LeaseUntil: model.LeaseUntil, AvailableAt: model.AvailableAt, LastError: model.LastError,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func (r *decisionJobRepo) CountActiveByUser(ctx context.Context, userID int64) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Joins("JOIN decision_case ON decision_case.id = decision_job.case_id").
		Where("decision_case.user_id = ? AND decision_job.status IN ?", userID,
			[]string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning)}).
		Count(&count).Error
	return int(count), err
}

var _ port.DecisionJobRepository = (*decisionJobRepo)(nil)
