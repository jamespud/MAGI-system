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

func (r *caseRepo) CommitCaseControl(ctx context.Context, caseID string, target entity.CaseStatus) (bool, error) {
	return (&magiRepository{db: r.db}).CommitCaseControl(ctx, caseID, target)
}

func (r *caseRepo) ResumeCaseControl(ctx context.Context, caseID string) (bool, error) {
	return (&magiRepository{db: r.db}).ResumeCaseControl(ctx, caseID)
}

func (r *magiRepository) ResumeCaseControl(ctx context.Context, caseID string) (bool, error) {
	return r.commitCaseControl(ctx, caseID, "", true)
}

// External control contends on the same Job -> Case locks as owned commits.
// Even if cancellation delivery never arrives, the persisted owner is gone.
func (r *magiRepository) CommitCaseControl(ctx context.Context, caseID string, target entity.CaseStatus) (bool, error) {
	return r.commitCaseControl(ctx, caseID, target, false)
}

func (r *magiRepository) commitCaseControl(ctx context.Context, caseID string, target entity.CaseStatus, resume bool) (bool, error) {
	if caseID == "" || (!resume && target != entity.CaseStatusPaused && target != entity.CaseStatusCancelled) {
		return false, fmt.Errorf("invalid case control")
	}
	var jobID, pausedFrom string
	var generation int64
	bodySucceeded := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job DecisionJobModel
		jobErr := gorm.ErrRecordNotFound
		// Narrow SQLite fixtures may persist only generation-0 Case rows.
		if tx.Dialector.Name() == "mysql" || tx.Migrator().HasTable(&DecisionJobModel{}) {
			jobErr = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("case_id = ?", caseID).First(&job).Error
		}
		if jobErr != nil && !errors.Is(jobErr, gorm.ErrRecordNotFound) {
			return jobErr
		}
		var c CaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", caseID).First(&c).Error; err != nil {
			return err
		}
		if jobErr != nil && c.ExecutionGeneration > 0 {
			return port.ErrLeaseLost
		}
		if resume {
			if c.Status != string(entity.CaseStatusPaused) || (jobErr == nil && job.Status != string(entity.DecisionJobPaused)) {
				return port.ErrLeaseLost
			}
			target = entity.CaseStatus(c.PausedFromStatus)
			if target == "" {
				target = entity.CaseStatusDraft
			}
		}
		if !resume && c.Status != string(target) && isPublicTerminalCaseStatus(entity.CaseStatus(c.Status)) {
			return port.ErrLeaseLost
		}
		if c.Status == string(entity.CaseStatusPaused) && target == entity.CaseStatusPaused {
			pausedFrom = c.PausedFromStatus
		} else if target == entity.CaseStatusPaused {
			pausedFrom = c.Status
		}
		updates := map[string]any{"status": string(target), "updated_at": time.Now()}
		if target == entity.CaseStatusPaused {
			updates["paused_from_status"] = pausedFrom
		}
		if resume {
			updates["paused_from_status"] = ""
		}
		if err := tx.Model(&CaseModel{}).Where("id = ?", caseID).Updates(updates).Error; err != nil {
			return err
		}
		generation = c.ExecutionGeneration
		if jobErr == nil {
			jobID = job.ID
			if job.ExecutionGeneration != generation {
				return fmt.Errorf("case control: Job/Case generation mismatch")
			}
			status := entity.DecisionJobCancelled
			if target == entity.CaseStatusPaused {
				status = entity.DecisionJobPaused
			}
			if resume {
				status = entity.DecisionJobQueued
			}
			jobUpdates := map[string]any{"status": string(status), "worker_id": "", "lease_until": nil, "updated_at": time.Now()}
			if resume {
				jobUpdates["available_at"] = time.Now()
			}
			if err := tx.Model(&job).Updates(jobUpdates).Error; err != nil {
				return err
			}
		}
		bodySucceeded = true
		return nil
	})
	if errors.Is(err, port.ErrLeaseLost) {
		return false, nil
	}
	if err == nil {
		return true, nil
	}
	if !bodySucceeded {
		return false, err
	}
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), claimRecoveryTimeout)
	defer cancel()
	confirmed := false
	recoverErr := r.db.WithContext(recovery).Transaction(func(tx *gorm.DB) error {
		var c CaseModel
		if err := tx.Where("id = ? AND status = ? AND execution_generation = ?", caseID, string(target), generation).First(&c).Error; err != nil {
			return err
		}
		if target == entity.CaseStatusPaused && c.PausedFromStatus != pausedFrom {
			return fmt.Errorf("case control: pre-pause status changed")
		}
		if jobID != "" {
			status := entity.DecisionJobCancelled
			if target == entity.CaseStatusPaused {
				status = entity.DecisionJobPaused
			}
			if resume {
				status = entity.DecisionJobQueued
			}
			var job DecisionJobModel
			if err := tx.Where("id = ? AND case_id = ? AND execution_generation = ? AND status = ? AND worker_id = '' AND lease_until IS NULL", jobID, caseID, generation, string(status)).First(&job).Error; err != nil {
				return err
			}
		}
		confirmed = true
		return nil
	})
	if recoverErr == nil && confirmed {
		return true, nil
	}
	return false, fmt.Errorf("%w: control: %v; confirmation: %v", port.ErrCommitOutcomeUnknown, err, recoverErr)
}

// reconcileTerminalJob never allocates ownership. It repairs historical
// terminal Cases whose Job settlement was split from the terminal transaction.
func reconcileTerminalJob(tx *gorm.DB, job *DecisionJobModel, c *CaseModel) (bool, error) {
	status := entity.CaseStatus(c.Status)
	if !isPublicTerminalCaseStatus(status) && status != entity.CaseStatusPaused {
		return false, nil
	}
	target := entity.DecisionJobFailed
	switch status {
	case entity.CaseStatusPaused:
		target = entity.DecisionJobPaused
	case entity.CaseStatusCancelled:
		target = entity.DecisionJobCancelled
	case entity.CaseStatusResolved, entity.CaseStatusDeadlocked:
		target = entity.DecisionJobSucceeded
		var count int64
		// Generation 0 events are accepted only for historical reconciliation;
		// this never attributes them to a generation or authorizes execution.
		if err := tx.Model(&EventModel{}).Where("case_id = ? AND type = ? AND execution_generation IN ?", c.ID, string(entity.EventCaseCompleted), []int64{0, c.ExecutionGeneration}).Count(&count).Error; err != nil {
			return true, err
		}
		if count == 0 {
			return true, fmt.Errorf("terminal Job reconciliation: completion event missing")
		}
		if status == entity.CaseStatusResolved {
			if err := tx.Model(&ResolutionModel{}).Where("case_id = ? AND execution_generation = ?", c.ID, c.ExecutionGeneration).Count(&count).Error; err != nil {
				return true, err
			}
			if count != 1 {
				return true, fmt.Errorf("terminal Job reconciliation: resolution generation mismatch")
			}
		}
	}
	if job.ExecutionGeneration != c.ExecutionGeneration {
		return true, fmt.Errorf("terminal Job reconciliation: generation mismatch")
	}
	err := tx.Model(job).Updates(map[string]any{"status": string(target), "worker_id": "", "lease_until": nil, "updated_at": time.Now()}).Error
	return true, err
}

var _ port.CaseControlCommitter = (*magiRepository)(nil)
var _ port.CaseControlCommitter = (*caseRepo)(nil)
var _ port.CaseResumeCommitter = (*caseRepo)(nil)
