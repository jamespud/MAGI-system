package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// databaseNow is deliberately called AFTER both ownership locks are acquired.
// MySQL NOW is statement-scoped: a separate statement avoids using the time
// from a locking statement that spent the remainder of the lease waiting.
func databaseNow(tx *gorm.DB) (time.Time, error) {
	if tx.Dialector.Name() != "mysql" {
		return time.Now(), nil
	}
	var now time.Time
	err := tx.Raw("SELECT CURRENT_TIMESTAMP(6)").Scan(&now).Error
	return now, err
}

func lockExecutionRows(tx *gorm.DB, jobID, caseID string) (*DecisionJobModel, *CaseModel, error) {
	var job DecisionJobModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND case_id = ?", jobID, caseID).First(&job).Error; err != nil {
		return nil, nil, err
	}
	var c CaseModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", caseID).First(&c).Error; err != nil {
		return nil, nil, err
	}
	return &job, &c, nil
}

func lockActiveExecution(tx *gorm.DB, owner *entity.ExecutionContext) (*DecisionJobModel, *CaseModel, error) {
	if !owner.IsDurable() {
		return nil, nil, port.ErrLeaseLost
	}
	job, c, err := lockExecutionRows(tx, owner.JobID, owner.CaseID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, port.ErrLeaseLost
	}
	if err != nil {
		return nil, nil, err
	}
	now, err := databaseNow(tx)
	if err != nil {
		return nil, nil, err
	}
	if job.Status != string(entity.DecisionJobRunning) || job.WorkerID != owner.WorkerID ||
		job.ExecutionGeneration != owner.ExecutionGeneration || c.ExecutionGeneration != owner.ExecutionGeneration ||
		job.LeaseUntil == nil || !job.LeaseUntil.After(now) {
		return nil, nil, port.ErrLeaseLost
	}
	return job, c, nil
}

func expectedCaseStatus(c *CaseModel, expected []entity.CaseStatus) bool {
	for _, s := range expected {
		if c.Status == string(s) {
			return true
		}
	}
	return false
}

func settleExecutionJob(tx *gorm.DB, job *DecisionJobModel, status entity.DecisionJobStatus, lastError string) error {
	result := tx.Model(job).Updates(map[string]any{"status": string(status), "worker_id": "", "lease_until": nil, "last_error": lastError, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return port.ErrLeaseLost
	}
	return nil
}

func validateOwnedEvent(owner *entity.ExecutionContext, event *entity.MagiEvent) error {
	if !owner.IsDurable() {
		return port.ErrLeaseLost
	}
	if event == nil || event.ID == "" || event.CaseID != owner.CaseID || event.ExecutionGeneration != owner.ExecutionGeneration {
		return fmt.Errorf("owned case commit: event identity/generation does not match owner")
	}
	return nil
}

// caseCommit separates known rollback from COMMIT ambiguity. The callback
// writes one event with a stable identity; confirmation uses a fresh snapshot.
func (r *magiRepository) caseCommit(ctx context.Context, owner *entity.ExecutionContext, event *entity.MagiEvent, target entity.CaseStatus, terminal bool, resolution *entity.Resolution, write func(*gorm.DB, *DecisionJobModel, *CaseModel) error) (bool, error) {
	if r.db == nil {
		return false, port.ErrLeaseLost
	}
	originalSeq := event.Seq
	bodySucceeded := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, c, err := lockActiveExecution(tx, owner)
		if err != nil {
			return err
		}
		if err := write(tx, job, c); err != nil {
			return err
		}
		bodySucceeded = true
		return nil
	})
	if err == nil {
		return true, nil
	}
	event.Seq = originalSeq
	if !bodySucceeded {
		if errors.Is(err, port.ErrLeaseLost) {
			return false, nil
		}
		return false, err
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimRecoveryTimeout)
	defer cancel()
	confirmed, recoverErr := r.confirmCaseCommit(recovery, owner, target, event, terminal, resolution)
	if recoverErr == nil && confirmed {
		return true, nil
	}
	return false, fmt.Errorf("%w: commit: %v; confirmation: %v", port.ErrCommitOutcomeUnknown, err, recoverErr)
}

func (r *magiRepository) confirmCaseCommit(ctx context.Context, owner *entity.ExecutionContext, target entity.CaseStatus, event *entity.MagiEvent, terminal bool, resolution *entity.Resolution) (bool, error) {
	confirmed := false
	var persistedSeq uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c CaseModel
		if err := tx.Where("id = ? AND status = ? AND execution_generation = ?", owner.CaseID, string(target), owner.ExecutionGeneration).First(&c).Error; err != nil {
			return err
		}
		var stored EventModel
		if err := tx.Where("id = ? AND case_id = ? AND execution_generation = ? AND type = ?", event.ID, owner.CaseID, owner.ExecutionGeneration, string(event.Type)).First(&stored).Error; err != nil {
			return err
		}
		if stored.PayloadJSON != string(event.Payload) || stored.Seq == 0 {
			return fmt.Errorf("completion event identity conflict")
		}
		if terminal {
			status := entity.DecisionJobSucceeded
			if target == entity.CaseStatusFailed {
				status = entity.DecisionJobFailed
			}
			var job DecisionJobModel
			if err := tx.Where("id = ? AND case_id = ? AND execution_generation = ? AND status = ? AND worker_id = '' AND lease_until IS NULL", owner.JobID, owner.CaseID, owner.ExecutionGeneration, string(status)).First(&job).Error; err != nil {
				return err
			}
		}
		if resolution != nil {
			var stored ResolutionModel
			if err := tx.Where("id = ? AND case_id = ? AND execution_generation = ?", resolution.ID, owner.CaseID, owner.ExecutionGeneration).First(&stored).Error; err != nil {
				return err
			}
		}
		persistedSeq = stored.Seq
		confirmed = true
		return nil
	})
	if err == nil && confirmed {
		event.Seq = persistedSeq
	}
	return confirmed, err
}

func (r *magiRepository) CommitStatusTransitionOwned(ctx context.Context, owner *entity.ExecutionContext, expected []entity.CaseStatus, target entity.CaseStatus, event *entity.MagiEvent) (bool, error) {
	if err := validateOwnedEvent(owner, event); err != nil {
		return false, err
	}
	if event.Type != entity.EventCaseStatusChanged || isPublicTerminalCaseStatus(target) || target == entity.CaseStatusPaused {
		return false, fmt.Errorf("owned status commit: invalid ordinary transition")
	}
	return r.caseCommit(ctx, owner, event, target, false, nil, func(tx *gorm.DB, _ *DecisionJobModel, c *CaseModel) error {
		if !expectedCaseStatus(c, expected) || isPublicTerminalCaseStatus(entity.CaseStatus(c.Status)) || c.Status == string(entity.CaseStatusPaused) {
			return port.ErrLeaseLost
		}
		result := tx.Model(&CaseModel{}).Where("id = ? AND execution_generation = ? AND status = ?", owner.CaseID, owner.ExecutionGeneration, c.Status).Updates(map[string]any{"status": string(target), "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return port.ErrLeaseLost
		}
		return createEventInTx(tx, event)
	})
}

func (r *magiRepository) ResetCaseForRetryOwned(ctx context.Context, owner *entity.ExecutionContext, expected []entity.CaseStatus) (bool, error) {
	if r.db == nil || !owner.IsDurable() {
		return false, port.ErrLeaseLost
	}
	reset := false
	bodySucceeded := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, c, err := lockActiveExecution(tx, owner)
		if err != nil {
			return err
		}
		if !expectedCaseStatus(c, expected) || isPublicTerminalCaseStatus(entity.CaseStatus(c.Status)) || c.Status == string(entity.CaseStatusPaused) {
			return port.ErrLeaseLost
		}
		if c.Status != string(entity.CaseStatusDraft) {
			result := tx.Model(&CaseModel{}).Where("id = ? AND execution_generation = ? AND status = ?", owner.CaseID, owner.ExecutionGeneration, c.Status).Updates(map[string]any{"status": string(entity.CaseStatusDraft), "updated_at": time.Now()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return port.ErrLeaseLost
			}
		}
		reset, bodySucceeded = true, true
		return nil
	})
	if errors.Is(err, port.ErrLeaseLost) {
		return false, nil
	}
	if err != nil && bodySucceeded {
		return false, fmt.Errorf("%w: retry reset: %v", port.ErrCommitOutcomeUnknown, err)
	}
	return reset && err == nil, err
}

func (r *magiRepository) CommitTerminalOwned(ctx context.Context, owner *entity.ExecutionContext, expected, target entity.CaseStatus, resolution *entity.Resolution, event *entity.MagiEvent) (bool, error) {
	if err := validateOwnedEvent(owner, event); err != nil {
		return false, err
	}
	if (target != entity.CaseStatusResolved && target != entity.CaseStatusDeadlocked) || event.Type != entity.EventCaseCompleted {
		return false, fmt.Errorf("owned terminal commit: invalid terminal outcome")
	}
	if target == entity.CaseStatusResolved && resolution == nil {
		return false, fmt.Errorf("owned terminal commit: resolution required")
	}
	if target == entity.CaseStatusDeadlocked && resolution != nil {
		return false, fmt.Errorf("owned terminal commit: deadlock must not carry a resolution")
	}
	if resolution != nil && (resolution.ID == "" || resolution.CaseID != owner.CaseID || resolution.ExecutionGeneration != owner.ExecutionGeneration) {
		return false, fmt.Errorf("owned terminal commit: resolution identity/generation does not match owner")
	}
	return r.caseCommit(ctx, owner, event, target, true, resolution, func(tx *gorm.DB, job *DecisionJobModel, c *CaseModel) error {
		if !expectedCaseStatus(c, []entity.CaseStatus{expected}) || isPublicTerminalCaseStatus(entity.CaseStatus(c.Status)) || c.Status == string(entity.CaseStatusPaused) {
			return port.ErrLeaseLost
		}
		if resolution != nil {
			if err := verifyResolutionReferences(tx, resolution); err != nil {
				return err
			}
			model := resolutionModel(resolution)
			if err := tx.Create(&model).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&CaseModel{}).Where("id = ? AND execution_generation = ? AND status = ?", owner.CaseID, owner.ExecutionGeneration, string(expected)).Updates(map[string]any{"status": string(target), "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return port.ErrLeaseLost
		}
		if err := createEventInTx(tx, event); err != nil {
			return err
		}
		return settleExecutionJob(tx, job, entity.DecisionJobSucceeded, "")
	})
}

func (r *magiRepository) ExecutionSettled(ctx context.Context, owner *entity.ExecutionContext) (bool, error) {
	if !owner.IsDurable() {
		return false, port.ErrLeaseLost
	}
	settled := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job DecisionJobModel
		if err := tx.Where("id = ? AND case_id = ? AND execution_generation = ?", owner.JobID, owner.CaseID, owner.ExecutionGeneration).First(&job).Error; err != nil {
			return err
		}
		var c CaseModel
		if err := tx.Where("id = ? AND execution_generation = ?", owner.CaseID, owner.ExecutionGeneration).First(&c).Error; err != nil {
			return err
		}
		if job.Status != string(entity.DecisionJobSucceeded) || job.WorkerID != "" || job.LeaseUntil != nil || (c.Status != string(entity.CaseStatusResolved) && c.Status != string(entity.CaseStatusDeadlocked)) {
			return nil
		}
		var count int64
		// Settlement clears worker_id; the immutable Claim record retains the
		// worker identity needed to authenticate an execution-result replay.
		if err := tx.Model(&DecisionJobClaimModel{}).Where("job_id = ? AND case_id = ? AND execution_generation = ? AND worker_id = ?", owner.JobID, owner.CaseID, owner.ExecutionGeneration, owner.WorkerID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
		if err := tx.Model(&EventModel{}).Where("case_id = ? AND execution_generation = ? AND type = ?", owner.CaseID, owner.ExecutionGeneration, string(entity.EventCaseCompleted)).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
		if c.Status == string(entity.CaseStatusResolved) {
			if err := tx.Model(&ResolutionModel{}).Where("case_id = ? AND execution_generation = ?", owner.CaseID, owner.ExecutionGeneration).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return nil
			}
		}
		settled = true
		return nil
	})
	return settled, err
}

var _ port.OwnedCaseCommitter = (*magiRepository)(nil)
var _ port.ExecutionSettlementReader = (*magiRepository)(nil)
