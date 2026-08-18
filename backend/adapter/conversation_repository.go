package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type conversationRepo struct{ db *gorm.DB }

// NewConversationRepository returns a DB-backed ConversationRepository.
func NewConversationRepository(db *gorm.DB) port.ConversationRepository {
	return &conversationRepo{db: db}
}

func (r *conversationRepo) Create(ctx context.Context, conv *entity.Conversation) error {
	now := time.Now()
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	conv.UpdatedAt = conv.CreatedAt
	return r.db.WithContext(ctx).Create(conversationToModel(conv)).Error
}

func (r *conversationRepo) Get(ctx context.Context, id string) (*entity.Conversation, error) {
	var m ConversationModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return conversationFromModel(&m), nil
}

func (r *conversationRepo) ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.Conversation, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var models []ConversationModel
	q := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Limit(limit).Offset(offset)
	if err := q.Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.Conversation, len(models))
	for i := range models {
		out[i] = conversationFromModel(&models[i])
	}
	return out, nil
}

func (r *conversationRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ?", id).Delete(&ConversationMessageModel{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&ConversationModel{}).Error
	})
}

func (r *conversationRepo) AppendMessage(ctx context.Context, msg *entity.ConversationMessage) error {
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if err := r.db.WithContext(ctx).Create(conversationMessageToModel(msg)).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&ConversationModel{}).
		Where("id = ?", msg.ConversationID).
		Update("updated_at", time.Now()).Error
}

func (r *conversationRepo) ListMessages(ctx context.Context, conversationID string, limit int) ([]*entity.ConversationMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var models []ConversationMessageModel
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at ASC, id ASC").Limit(limit).
		Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.ConversationMessage, len(models))
	for i := range models {
		out[i] = conversationMessageFromModel(&models[i])
	}
	return out, nil
}

func conversationToModel(c *entity.Conversation) *ConversationModel {
	return &ConversationModel{
		ID: c.ID, UserID: c.UserID, Title: c.Title,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func conversationFromModel(m *ConversationModel) *entity.Conversation {
	return &entity.Conversation{
		ID: m.ID, UserID: m.UserID, Title: m.Title,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func conversationMessageToModel(m *entity.ConversationMessage) *ConversationMessageModel {
	return &ConversationMessageModel{
		ID: m.ID, ConversationID: m.ConversationID, UserID: m.UserID,
		Role: m.Role, Content: m.Content, CaseID: m.CaseID, CreatedAt: m.CreatedAt,
	}
}

func conversationMessageFromModel(m *ConversationMessageModel) *entity.ConversationMessage {
	return &entity.ConversationMessage{
		ID: m.ID, ConversationID: m.ConversationID, UserID: m.UserID,
		Role: m.Role, Content: m.Content, CaseID: m.CaseID, CreatedAt: m.CreatedAt,
	}
}
