package magi

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type pluginBindingRepo struct{ db *gorm.DB }

func NewPluginBindingRepository(db *gorm.DB) port.PluginBindingRepository {
	return &pluginBindingRepo{db: db}
}

func (r *pluginBindingRepo) Create(ctx context.Context, b *entity.PluginBinding) error {
	if b.ID == "" {
		b.ID = "pb-" + uuid.NewString()
	}
	now := time.Now()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	b.UpdatedAt = now
	m := PluginBindingModel{ID: b.ID, UserID: b.UserID, PluginID: b.PluginID, ToolID: b.ToolID, IsDraft: b.IsDraft, Enabled: b.Enabled, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *pluginBindingRepo) Get(ctx context.Context, id string) (*entity.PluginBinding, error) {
	var m PluginBindingModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return pluginBindingFromModel(&m), nil
}

func (r *pluginBindingRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.PluginBinding, error) {
	var models []PluginBindingModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.PluginBinding, len(models))
	for i := range models {
		out[i] = pluginBindingFromModel(&models[i])
	}
	return out, nil
}

func (r *pluginBindingRepo) UpdateEnabled(ctx context.Context, id string, enabled bool) error {
	return r.db.WithContext(ctx).Model(&PluginBindingModel{}).Where("id = ?", id).Updates(map[string]any{
		"enabled": enabled, "updated_at": time.Now(),
	}).Error
}

func (r *pluginBindingRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&PluginBindingModel{}, "id = ?", id).Error
}

func pluginBindingFromModel(m *PluginBindingModel) *entity.PluginBinding {
	return &entity.PluginBinding{
		ID: m.ID, UserID: m.UserID, PluginID: m.PluginID, ToolID: m.ToolID,
		IsDraft: m.IsDraft, Enabled: m.Enabled, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

var _ port.PluginBindingRepository = (*pluginBindingRepo)(nil)
