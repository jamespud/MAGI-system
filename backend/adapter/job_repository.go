package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

// Admit atomically enqueues or requeues a decision job under a per-user limit.
// It takes a per-user database row lock so two replicas cannot both pass the
// limit, then derives the authority concurrency truth from queued/running
// DecisionJob rows (not from a materialized counter that leaks on crash).
func (r *decisionJobRepo) Admit(ctx context.Context, caseID string, maxAttempts, perUserLimit int) (*entity.DecisionJob, bool, error) {
	if caseID == "" {
		return nil, false, fmt.Errorf("decision job: case ID is required")
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultJobAttempts
	}
	var jobEntity *entity.DecisionJob
	admitted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Read the authoritative owner for the case.
		// On MySQL this must be a locking read: a plain consistent read would
		// establish this transaction's REPEATABLE READ snapshot before the
		// per-user admission lock is taken below, so a replica that waits on
		// that lock could still count a stale view and both pass the limit.
		// A locking read never creates a snapshot; the queued/running count
		// below then opens its read view only after the lock is held.
		var caseModel CaseModel
		caseQuery := tx.Where("id = ?", caseID)
		if tx.Dialector.Name() == "mysql" {
			caseQuery = caseQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := caseQuery.First(&caseModel).Error; err != nil {
			return err
		}
		userID := caseModel.UserID
		enforceLimit := userID != 0 && perUserLimit > 0

		// Serialize admission per user. In MySQL we hold a row lock on the
		// admission mutex; SQLite's single-writer transaction provides the
		// equivalent serialization for tests.
		if enforceLimit {
			lock := RunAdmissionLockModel{UserID: userID, UpdatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "user_id"}},
				DoUpdates: clause.Assignments(map[string]any{"updated_at": gorm.Expr("updated_at")}),
			}).Create(&lock).Error; err != nil {
				return err
			}
			if tx.Dialector.Name() == "mysql" {
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
					Where("user_id = ?", userID).
					First(&RunAdmissionLockModel{}).Error; err != nil {
					return err
				}
			}
		}

		// Idempotent: an already-active/queued/succeeded job for this case wins
		// without consuming a new capacity decision.
		var model DecisionJobModel
		findErr := tx.Where("case_id = ?", caseID).First(&model).Error
		if findErr == nil {
			switch entity.DecisionJobStatus(model.Status) {
			case entity.DecisionJobQueued, entity.DecisionJobRunning, entity.DecisionJobSucceeded:
				jobEntity = jobFromModel(&model)
				admitted = true
				return nil
			}
			// Terminal retryable (failed/cancelled/paused) falls through so
			// capacity can be re-checked before resetting to queued.
		} else if !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		// Count this user's queued/running jobs under the same transaction.
		if enforceLimit {
			var count int64
			if err := tx.Model(&DecisionJobModel{}).
				Joins("JOIN decision_case ON decision_case.id = decision_job.case_id").
				Where("decision_case.user_id = ? AND decision_job.status IN ?", userID,
					[]string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning)}).
				Count(&count).Error; err != nil {
				return err
			}
			if int(count) >= perUserLimit {
				return nil
			}
		}

		now := time.Now()
		if findErr == nil {
			// Reset a terminal retryable job to queued.
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
			if err := tx.Model(&DecisionJobModel{}).Where("id = ?", model.ID).Updates(updates).Error; err != nil {
				return err
			}
			jobEntity = jobFromModel(&model)
			jobEntity.Status = entity.DecisionJobQueued
			jobEntity.Attempt = 0
			jobEntity.WorkerID = ""
			jobEntity.LeaseUntil = nil
			jobEntity.AvailableAt = now
			jobEntity.LastError = ""
			jobEntity.UpdatedAt = now
			admitted = true
			return nil
		}

		// Create a new queued job.
		newModel := DecisionJobModel{
			ID: "job-" + uuid.NewString(), CaseID: caseID, Status: string(entity.DecisionJobQueued),
			MaxAttempts: maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&newModel).Error; err != nil {
			// A concurrent admit may have won the unique case_id race.
			if existing, getErr := r.getByCase(tx, caseID); getErr == nil {
				jobEntity = existing
				admitted = true
				return nil
			}
			return err
		}
		jobEntity = jobFromModel(&newModel)
		admitted = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return jobEntity, admitted, nil
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
		return fmt.Errorf("decision job: heartbeat: %w", port.ErrLeaseLost)
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
		return fmt.Errorf("decision job: mark succeeded: %w", port.ErrLeaseLost)
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
		return fmt.Errorf("decision job: mark failed: %w", port.ErrLeaseLost)
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

// MarkPaused parks a queued/running durable job. The execution context is
// cancelled by the run manager; the job stays out of the runnable set until
// ResumeQueued.
func (r *decisionJobRepo) MarkPaused(ctx context.Context, jobID string) error {
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status IN ?", jobID,
			[]string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning)}).
		Updates(map[string]any{"status": string(entity.DecisionJobPaused), "worker_id": "", "lease_until": nil, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: cannot pause")
	}
	return nil
}

// ResumeQueued returns a paused job to the runnable set.
func (r *decisionJobRepo) ResumeQueued(ctx context.Context, jobID string) error {
	result := r.db.WithContext(ctx).Model(&DecisionJobModel{}).
		Where("id = ? AND status = ?", jobID, string(entity.DecisionJobPaused)).
		Updates(map[string]any{"status": string(entity.DecisionJobQueued), "available_at": time.Now(), "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("decision job: cannot resume")
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
		Order("created_at ASC, id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.DecisionJob, len(models))
	for i := range models {
		out[i] = jobFromModel(&models[i])
	}
	return out, nil
}

func (r *decisionJobRepo) GetByCase(ctx context.Context, caseID string) (*entity.DecisionJob, error) {
	return r.getByCase(r.db.WithContext(ctx), caseID)
}

func (r *decisionJobRepo) getByCase(db *gorm.DB, caseID string) (*entity.DecisionJob, error) {
	var model DecisionJobModel
	if err := db.First(&model, "case_id = ?", caseID).Error; err != nil {
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

var errFinalFailureFence = errors.New("decision job: final failure fence lost")

// CommitFinalFailure is the durable terminal write for an exhausted worker
// attempt. The job lease, case status, event cursor, and CASE_FAILED event all
// commit or roll back together.
func (r *decisionJobRepo) CommitFinalFailure(ctx context.Context, jobID, workerID, caseID string, expectedCaseStatuses []entity.CaseStatus, lastError string, event *entity.MagiEvent) (bool, error) {
	if event == nil {
		return false, fmt.Errorf("decision job: final failure event is required")
	}
	allowedStatuses := make([]string, 0, len(expectedCaseStatuses))
	for _, status := range expectedCaseStatuses {
		if !isPublicTerminalCaseStatus(status) {
			allowedStatuses = append(allowedStatuses, string(status))
		}
	}
	if len(allowedStatuses) == 0 {
		return false, nil
	}
	originalSeq := event.Seq
	committed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		jobResult := tx.Model(&DecisionJobModel{}).
			Where("id = ? AND case_id = ? AND status = ? AND worker_id = ?", jobID, caseID, string(entity.DecisionJobRunning), workerID).
			Updates(map[string]any{
				"status": string(entity.DecisionJobFailed), "worker_id": "", "lease_until": nil,
				"last_error": lastError, "updated_at": now,
			})
		if jobResult.Error != nil {
			return jobResult.Error
		}
		if jobResult.RowsAffected != 1 {
			return errFinalFailureFence
		}
		caseResult := tx.Model(&CaseModel{}).
			Where("id = ? AND status IN ? AND status NOT IN ?", caseID, allowedStatuses, publicTerminalCaseStatuses()).
			Updates(map[string]any{"status": string(entity.CaseStatusFailed), "updated_at": now})
		if caseResult.Error != nil {
			return caseResult.Error
		}
		if caseResult.RowsAffected != 1 {
			return errFinalFailureFence
		}
		if err := createEventInTx(tx, event); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		// createEventInTx assigns Seq before the event INSERT. Never leave an
		// uncommitted sequence on the caller's event pointer.
		event.Seq = originalSeq
		if errors.Is(err, errFinalFailureFence) {
			return false, nil
		}
		return false, err
	}
	if !committed {
		event.Seq = originalSeq
	}
	return committed, nil
}

func isPublicTerminalCaseStatus(status entity.CaseStatus) bool {
	switch status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusCancelled, entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv,
		entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

func publicTerminalCaseStatuses() []string {
	return []string{
		string(entity.CaseStatusResolved), string(entity.CaseStatusMemoryIndexed),
		string(entity.CaseStatusFailed), string(entity.CaseStatusCancelled),
		string(entity.CaseStatusTimedOut), string(entity.CaseStatusInsufficientEv),
		string(entity.CaseStatusDeadlocked),
	}
}
