package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// ConversationRepository persists multi-turn assistant conversations.
type ConversationRepository interface {
	Create(ctx context.Context, conv *entity.Conversation) error
	Get(ctx context.Context, id string) (*entity.Conversation, error)
	ListByUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.Conversation, error)
	Delete(ctx context.Context, id string) error
	AppendMessage(ctx context.Context, msg *entity.ConversationMessage) error
	ListMessages(ctx context.Context, conversationID string, limit int) ([]*entity.ConversationMessage, error)
}
