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

const defaultRagIndexJobAttempts = 3

type ragIndexJobRepo struct {
	db *gorm.DB
}

func NewRagIndexJobRepository(db *gorm.DB) port.RagIndexJobRepository {
	return &ragIndexJobRepo{db: db}
}

func (r *ragIndexJobRepo) Enqueue(ctx context.Context, kind entity.RagIndexJobKind, source, sourceRef string, maxAttempts int) (*entity.RagIndexJob, error) {
	if sourceRef == "" {
		return nil, fmt.Errorf("rag index job: source_ref is required")
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultRagIndexJobAttempts
	}
	now := time.Now()
	model := RagIndexJobModel{
		ID: "ri-" + uuid.NewString(), Kind: string(kind), Source: source, SourceRef: sourceRef,
		Status: string(entity.RagIndexJobQueued), MaxAttempts: maxAttempts,
		AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return nil, err
	}
	return ragIndexJobFromModel(&model), nil
}

func (r *ragIndexJobRepo) Claim(ctx context.Context, jobID, workerID string, leaseUntil time.Time) (*entity.RagIndexJob, bool, error) {
	var claimed RagIndexJobModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND status = ? AND available_at <= ?", jobID, string(entity.RagIndexJobQueued), time.Now()).First(&claimed).Error; err != nil {
			return err
		}
		result := tx.Model(&RagIndexJobModel{}).
			Where("id = ? AND status = ?", jobID, string(entity.RagIndexJobQueued)).
			Updates(map[string]any{
				"status": string(entity.RagIndexJobRunning), "worker_id": workerID,
				"lease_until": leaseUntil, "attempt": claimed.Attempt + 1, "updated_at": time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		claimed.Status = string(entity.RagIndexJobRunning)
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
	return ragIndexJobFromModel(&claimed), true, nil
}

func (r *ragIndexJobRepo) Heartbeat(ctx context.Context, jobID, workerID string, leaseUntil time.Time) error {
	result := r.db.WithContext(ctx).Model(&RagIndexJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.RagIndexJobRunning), workerID).
		Updates(map[string]any{"lease_until": leaseUntil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("rag index job: lease lost")
	}
	return nil
}

func (r *ragIndexJobRepo) MarkSucceeded(ctx context.Context, jobID, workerID string) error {
	result := r.db.WithContext(ctx).Model(&RagIndexJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.RagIndexJobRunning), workerID).
		Updates(map[string]any{"status": string(entity.RagIndexJobSucceeded), "worker_id": "", "lease_until": nil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("rag index job: cannot mark succeeded")
	}
	return nil
}

func (r *ragIndexJobRepo) MarkFailed(ctx context.Context, jobID, workerID, lastError string, retryAt *time.Time) error {
	updates := map[string]any{"worker_id": "", "lease_until": nil, "last_error": lastError, "updated_at": time.Now()}
	if retryAt != nil {
		updates["status"] = string(entity.RagIndexJobQueued)
		updates["available_at"] = *retryAt
	} else {
		updates["status"] = string(entity.RagIndexJobFailed)
	}
	result := r.db.WithContext(ctx).Model(&RagIndexJobModel{}).
		Where("id = ? AND status = ? AND worker_id = ?", jobID, string(entity.RagIndexJobRunning), workerID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("rag index job: cannot mark failed")
	}
	return nil
}

func (r *ragIndexJobRepo) Cancel(ctx context.Context, jobID string) error {
	result := r.db.WithContext(ctx).Model(&RagIndexJobModel{}).
		Where("id = ? AND status IN ?", jobID,
			[]string{string(entity.RagIndexJobQueued), string(entity.RagIndexJobRunning)}).
		Updates(map[string]any{"status": string(entity.RagIndexJobCancelled), "worker_id": "", "lease_until": nil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *ragIndexJobRepo) RequeueExpired(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Model(&RagIndexJobModel{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", string(entity.RagIndexJobRunning), now).
		Updates(map[string]any{"status": string(entity.RagIndexJobQueued), "worker_id": "", "lease_until": nil, "available_at": now, "updated_at": now}).Error
}

func (r *ragIndexJobRepo) ListRunnable(ctx context.Context, now time.Time) ([]*entity.RagIndexJob, error) {
	var models []RagIndexJobModel
	if err := r.db.WithContext(ctx).
		Where("status = ? AND available_at <= ?", string(entity.RagIndexJobQueued), now).
		Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.RagIndexJob, len(models))
	for i := range models {
		out[i] = ragIndexJobFromModel(&models[i])
	}
	return out, nil
}

func ragIndexJobFromModel(m *RagIndexJobModel) *entity.RagIndexJob {
	return &entity.RagIndexJob{
		ID: m.ID, Kind: entity.RagIndexJobKind(m.Kind), Source: m.Source, SourceRef: m.SourceRef,
		Status: entity.RagIndexJobStatus(m.Status), Attempt: m.Attempt, MaxAttempts: m.MaxAttempts,
		WorkerID: m.WorkerID, LeaseUntil: m.LeaseUntil, AvailableAt: m.AvailableAt,
		LastError: m.LastError, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

var _ port.RagIndexJobRepository = (*ragIndexJobRepo)(nil)
