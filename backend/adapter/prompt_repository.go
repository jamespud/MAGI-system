package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// PromptTemplateModel persists versioned prompt templates (P2 D12).
type PromptTemplateModel struct {
	Key       string `gorm:"primaryKey;size:64"`
	Version   int    `gorm:"primaryKey"`
	Content   string `gorm:"type:longtext"`
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (PromptTemplateModel) TableName() string { return "prompt_template" }

type promptRepo struct{ db *gorm.DB }

// NewPromptRepository builds the DB-backed prompt repository.
func NewPromptRepository(db *gorm.DB) port.PromptRepository {
	return &promptRepo{db: db}
}

func (r *promptRepo) List(ctx context.Context) ([]*entity.PromptTemplate, error) {
	var models []PromptTemplateModel
	if err := r.db.WithContext(ctx).Order("`key` ASC, version DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.PromptTemplate, len(models))
	for i, m := range models {
		out[i] = promptFromModel(&m)
	}
	return out, nil
}

func (r *promptRepo) Get(ctx context.Context, key string) (*entity.PromptTemplate, error) {
	var m PromptTemplateModel
	if err := r.db.WithContext(ctx).Where("`key` = ? AND active = ?", key, true).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return promptFromModel(&m), nil
}

func (r *promptRepo) Save(ctx context.Context, key, content string) (*entity.PromptTemplate, error) {
	now := time.Now()
	var cur PromptTemplateModel
	err := r.db.WithContext(ctx).Where("`key` = ? AND active = ?", key, true).First(&cur).Error
	var nextVersion int
	if err == nil {
		nextVersion = cur.Version + 1
	} else {
		nextVersion = 1
	}
	return r.writeActive(ctx, key, nextVersion, content, now)
}

func (r *promptRepo) Restore(ctx context.Context, key, content string) (*entity.PromptTemplate, error) {
	now := time.Now()
	var cur PromptTemplateModel
	err := r.db.WithContext(ctx).Where("`key` = ? AND active = ?", key, true).First(&cur).Error
	nextVersion := 1
	if err == nil {
		nextVersion = cur.Version
	}
	return r.writeActive(ctx, key, nextVersion, content, now)
}

// writeActive marks every version of key inactive, then upserts the given
// version as active. Runs in a transaction.
func (r *promptRepo) writeActive(ctx context.Context, key string, version int, content string, now time.Time) (*entity.PromptTemplate, error) {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	if err := tx.Model(&PromptTemplateModel{}).Where("`key` = ?", key).Update("active", false).Error; err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	m := PromptTemplateModel{Key: key, Version: version, Content: content, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := tx.Where("`key` = ? AND version = ?", key, version).Delete(&PromptTemplateModel{}).Error; err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Create(&m).Error; err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit().Error; err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return promptFromModel(&m), nil
}

func promptFromModel(m *PromptTemplateModel) *entity.PromptTemplate {
	return &entity.PromptTemplate{
		Key: m.Key, Version: m.Version, Content: m.Content, Active: m.Active,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// DBPromptProvider is a port.PromptProvider backed by the repository.
type DBPromptProvider struct {
	repo port.PromptRepository
}

// NewDBPromptProvider adapts a repository to the runtime read-side.
func NewDBPromptProvider(repo port.PromptRepository) *DBPromptProvider {
	return &DBPromptProvider{repo: repo}
}

func (p *DBPromptProvider) Load(ctx context.Context, key string) (string, bool) {
	if p == nil || p.repo == nil {
		return "", false
	}
	t, err := p.repo.Get(ctx, key)
	if err != nil || t == nil {
		return "", false
	}
	return t.Content, true
}

var _ port.PromptProvider = (*DBPromptProvider)(nil)
