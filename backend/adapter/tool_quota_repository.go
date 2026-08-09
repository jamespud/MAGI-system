package magi

import (
	"context"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

type toolQuotaRepo struct {
	db *gorm.DB
}

// NewToolQuotaRepository returns the GORM-backed ToolQuotaRepository.
func NewToolQuotaRepository(db *gorm.DB) port.ToolQuotaRepository {
	return &toolQuotaRepo{db: db}
}

func (r *toolQuotaRepo) TryConsume(ctx context.Context, userID int64, toolName string, windowStart time.Time, limit int) (bool, error) {
	if limit <= 0 {
		return true, nil
	}
	var allowed bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m ToolQuotaCounterModel
		err := tx.Where("user_id = ? AND tool_name = ? AND window_start = ?", userID, toolName, windowStart).First(&m).Error
		if err == gorm.ErrRecordNotFound {
			m = ToolQuotaCounterModel{UserID: userID, ToolName: toolName, WindowStart: windowStart, Calls: 1}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
			allowed = true
			return nil
		}
		if err != nil {
			return err
		}
		if m.Calls >= limit {
			allowed = false
			return nil
		}
		if err := tx.Model(&ToolQuotaCounterModel{}).
			Where("user_id = ? AND tool_name = ? AND window_start = ?", userID, toolName, windowStart).
			Update("calls", m.Calls+1).Error; err != nil {
			return err
		}
		allowed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("tool quota: %w", err)
	}
	return allowed, nil
}

var _ port.ToolQuotaRepository = (*toolQuotaRepo)(nil)
