package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type knowledgeRepo struct{ db *gorm.DB }

// NewKnowledgeRepository returns a DB-backed KnowledgeRepository.
func NewKnowledgeRepository(db *gorm.DB) port.KnowledgeRepository {
	return &knowledgeRepo{db: db}
}

func (r *knowledgeRepo) Create(ctx context.Context, doc *entity.KnowledgeDoc) error {
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now()
	}
	doc.UpdatedAt = doc.CreatedAt
	return r.db.WithContext(ctx).Create(knowledgeDocToModel(doc)).Error
}

func (r *knowledgeRepo) Get(ctx context.Context, id string) (*entity.KnowledgeDoc, error) {
	var m KnowledgeDocModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return knowledgeDocFromModel(&m), nil
}

func (r *knowledgeRepo) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.KnowledgeDoc, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var models []KnowledgeDocModel
	q := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset)
	if userID != 0 {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.KnowledgeDoc, len(models))
	for i := range models {
		out[i] = knowledgeDocFromModel(&models[i])
	}
	return out, nil
}

func (r *knowledgeRepo) Update(ctx context.Context, doc *entity.KnowledgeDoc) error {
	doc.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Save(knowledgeDocToModel(doc)).Error
}

func (r *knowledgeRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&KnowledgeDocModel{}).Error
}

func knowledgeDocToModel(d *entity.KnowledgeDoc) *KnowledgeDocModel {
	return &KnowledgeDocModel{
		ID: d.ID, UserID: d.UserID, Title: d.Title, Content: d.Content,
		SourceKind: d.SourceKind, SourceURL: d.SourceURL, Status: d.Status,
		Error: d.Error, Chunks: d.Chunks, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func knowledgeDocFromModel(m *KnowledgeDocModel) *entity.KnowledgeDoc {
	return &entity.KnowledgeDoc{
		ID: m.ID, UserID: m.UserID, Title: m.Title, Content: m.Content,
		SourceKind: m.SourceKind, SourceURL: m.SourceURL, Status: m.Status,
		Error: m.Error, Chunks: m.Chunks, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}
