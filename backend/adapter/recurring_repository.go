package magi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type recurringRepo struct{ db *gorm.DB }

func NewRecurringRepository(db *gorm.DB) port.RecurringRepository {
	return &recurringRepo{db: db}
}

func (r *recurringRepo) Create(ctx context.Context, rc *entity.RecurringCase) error {
	if rc.ID == "" {
		rc.ID = "rc-" + uuid.NewString()
	}
	now := time.Now()
	if rc.CreatedAt.IsZero() {
		rc.CreatedAt = now
	}
	rc.UpdatedAt = now
	return r.db.WithContext(ctx).Create(recurringToModel(rc)).Error
}

func (r *recurringRepo) Get(ctx context.Context, id string) (*entity.RecurringCase, error) {
	var m RecurringCaseModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return recurringFromModel(&m), nil
}

func (r *recurringRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.RecurringCase, error) {
	var models []RecurringCaseModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.RecurringCase, len(models))
	for i := range models {
		out[i] = recurringFromModel(&models[i])
	}
	return out, nil
}

func (r *recurringRepo) ListEnabled(ctx context.Context) ([]*entity.RecurringCase, error) {
	var models []RecurringCaseModel
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.RecurringCase, len(models))
	for i := range models {
		out[i] = recurringFromModel(&models[i])
	}
	return out, nil
}

func (r *recurringRepo) Update(ctx context.Context, rc *entity.RecurringCase) error {
	rc.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(&RecurringCaseModel{}).Where("id = ?", rc.ID).Updates(map[string]any{
		"name": rc.Name, "question": rc.Question, "context": rc.Background,
		"constraints_json": toJSON(rc.Constraints), "interval_millis": rc.Interval.Milliseconds(),
		"enabled": rc.Enabled, "updated_at": rc.UpdatedAt,
	}).Error
}

func (r *recurringRepo) UpdateEnabled(ctx context.Context, id string, enabled bool) error {
	return r.db.WithContext(ctx).Model(&RecurringCaseModel{}).Where("id = ?", id).Updates(map[string]any{
		"enabled": enabled, "updated_at": time.Now(),
	}).Error
}

func (r *recurringRepo) UpdateLastRun(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RecurringCaseModel{}).Where("id = ?", id).Updates(map[string]any{
		"last_run_at": at, "updated_at": time.Now(),
	}).Error
}

func (r *recurringRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&RecurringCaseModel{}, "id = ?", id).Error
}

func recurringToModel(rc *entity.RecurringCase) RecurringCaseModel {
	return RecurringCaseModel{
		ID: rc.ID, UserID: rc.UserID, Name: rc.Name, Question: rc.Question, Context: rc.Background,
		ConstraintsJSON: toJSON(rc.Constraints), IntervalMillis: rc.Interval.Milliseconds(),
		Enabled: rc.Enabled, LastRunAt: rc.LastRunAt, CreatedAt: rc.CreatedAt, UpdatedAt: rc.UpdatedAt,
	}
}

func recurringFromModel(m *RecurringCaseModel) *entity.RecurringCase {
	return &entity.RecurringCase{
		ID: m.ID, UserID: m.UserID, Name: m.Name, Question: m.Question, Background: m.Context,
		Constraints: fromJSON[[]entity.Constraint](m.ConstraintsJSON), Interval: time.Duration(m.IntervalMillis) * time.Millisecond,
		Enabled: m.Enabled, LastRunAt: m.LastRunAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

var _ port.RecurringRepository = (*recurringRepo)(nil)
