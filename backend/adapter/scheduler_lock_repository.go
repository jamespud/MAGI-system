package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type schedulerLockRepo struct {
	db *gorm.DB
}

// NewSchedulerLock returns the GORM-backed distributed scheduler lock.
func NewSchedulerLock(db *gorm.DB) port.SchedulerLock {
	return &schedulerLockRepo{db: db}
}

func (r *schedulerLockRepo) Acquire(ctx context.Context, name, owner string, ttl time.Duration) (bool, error) {
	if name == "" || owner == "" {
		return false, fmt.Errorf("scheduler lock: name and owner are required")
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now()
	until := now.Add(ttl)
	acquired := false

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m SchedulerLockModel
		query := tx.Where("name = ?", name)
		if tx.Dialector.Name() == "mysql" {
			// Locking read: concurrent takeovers serialize on this row instead of
			// racing on a snapshot taken before the lock was held.
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		switch err := query.First(&m).Error; {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&SchedulerLockModel{
				Name: name, Owner: owner, LeaseUntil: until, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
			acquired = true
			return nil
		case err != nil:
			return err
		}

		if m.LeaseUntil.After(now) && m.Owner != owner {
			return nil // another replica holds a live lease
		}
		// Take or renew: owner, lease and updated_at always move together, so a
		// takeover leaves a fresh timestamp (MySQL's left-to-right evaluation
		// used to leave it stale — see docs/reliability-hazard-audit.md §4.3).
		if err := tx.Model(&SchedulerLockModel{}).Where("name = ?", name).Updates(map[string]any{
			"owner": owner, "lease_until": until, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		acquired = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return acquired, nil
}

func (r *schedulerLockRepo) Release(ctx context.Context, name, owner string) error {
	return r.db.WithContext(ctx).Model(&SchedulerLockModel{}).
		Where("name = ? AND owner = ?", name, owner).
		Update("lease_until", time.Now()).Error
}

var _ port.SchedulerLock = (*schedulerLockRepo)(nil)
