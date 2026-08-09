package magi

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
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
	if r.db.Dialector.Name() == "mysql" {
		err := r.db.WithContext(ctx).Exec(
			`INSERT INTO magi_scheduler_lock (name, owner, lease_until, updated_at) VALUES (?, ?, ?, NOW())
			 ON DUPLICATE KEY UPDATE
			   owner = IF(lease_until < ?, VALUES(owner), owner),
			   lease_until = IF(lease_until < ?, VALUES(lease_until), lease_until),
			   updated_at = IF(lease_until < ?, NOW(), updated_at)`,
			name, owner, until, now, now, now).Error
		if err != nil {
			return false, err
		}
	} else {
		err := r.db.WithContext(ctx).Exec(
			`INSERT INTO magi_scheduler_lock (name, owner, lease_until, updated_at) VALUES (?, ?, ?, ?)
			 ON CONFLICT(name) DO UPDATE SET
			   owner = CASE WHEN lease_until < ? THEN excluded.owner ELSE owner END,
			   lease_until = CASE WHEN lease_until < ? THEN excluded.lease_until ELSE lease_until END,
			   updated_at = CASE WHEN lease_until < ? THEN excluded.updated_at ELSE updated_at END`,
			name, owner, until, now, now, now, now).Error
		if err != nil {
			return false, err
		}
	}
	var m SchedulerLockModel
	if err := r.db.WithContext(ctx).First(&m, "name = ?", name).Error; err != nil {
		return false, err
	}
	return m.Owner == owner && m.LeaseUntil.After(time.Now()), nil
}

func (r *schedulerLockRepo) Release(ctx context.Context, name, owner string) error {
	return r.db.WithContext(ctx).Model(&SchedulerLockModel{}).
		Where("name = ? AND owner = ?", name, owner).
		Update("lease_until", time.Now()).Error
}

var _ port.SchedulerLock = (*schedulerLockRepo)(nil)
