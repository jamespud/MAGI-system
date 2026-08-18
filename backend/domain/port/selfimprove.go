package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// SelfImproveRepository persists analyzed failure suggestions.
type SelfImproveRepository interface {
	Create(ctx context.Context, s *entity.SelfImproveSuggestion) error
	List(ctx context.Context) ([]*entity.SelfImproveSuggestion, error)
	Get(ctx context.Context, id string) (*entity.SelfImproveSuggestion, error)
	UpdateStatus(ctx context.Context, id, status string) error
}
