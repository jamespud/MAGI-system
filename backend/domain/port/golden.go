package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// GoldenRepository persists online-golden decision cases.
type GoldenRepository interface {
	Create(ctx context.Context, g *entity.GoldenCase) error
	List(ctx context.Context) ([]*entity.GoldenCase, error)
	Delete(ctx context.Context, id string) error
}
