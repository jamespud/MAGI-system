package magi

import (
	"context"

	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

// RunCounterModel is the shared per-user active-run counter. A single row per
// user makes check-and-increment atomic across replicas.
type RunCounterModel struct {
	UserID    int64 `gorm:"primaryKey"`
	ActiveRun int   `gorm:"not null;default:0"`
}

func (RunCounterModel) TableName() string { return "magi_user_run_counter" }

type runCounterRepo struct {
	db *gorm.DB
}

// NewRunCounterRepository returns the GORM-backed RunCounter.
func NewRunCounterRepository(db *gorm.DB) port.RunCounter {
	return &runCounterRepo{db: db}
}

var _ port.RunCounter = (*runCounterRepo)(nil)

func (r *runCounterRepo) Acquire(ctx context.Context, userID int64, limit int) (bool, error) {
	if limit <= 0 || userID == 0 {
		return true, nil
	}
	// Create the row if it does not exist yet. On a duplicate (another replica
	// won the race) the fall-through below runs the guarded increment instead,
	// so a just-freed slot is still usable.
	insertSQL := `INSERT INTO magi_user_run_counter (user_id, active_run) VALUES (?, 1)
	              ON DUPLICATE KEY UPDATE active_run = active_run`
	if r.db.Dialector.Name() != "mysql" {
		insertSQL = `INSERT OR IGNORE INTO magi_user_run_counter (user_id, active_run) VALUES (?, 1)`
	}
	ins := r.db.WithContext(ctx).Exec(insertSQL, userID)
	if ins.Error == nil && ins.RowsAffected > 0 {
		return true, nil
	}
	// Atomic path for an existing row: increment only while below the limit.
	// The WHERE clause is the lock; two replicas cannot both pass it.
	res := r.db.WithContext(ctx).Exec(
		`UPDATE magi_user_run_counter SET active_run = active_run + 1
		 WHERE user_id = ? AND active_run < ?`,
		userID, limit)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *runCounterRepo) Release(ctx context.Context, userID int64) error {
	if userID == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec(
		`UPDATE magi_user_run_counter SET active_run = active_run - 1 WHERE user_id = ? AND active_run > 0`,
		userID).Error
}
