package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// FSMBlueprintRepository persists the editable orchestration blueprint.
type FSMBlueprintRepository interface {
	Get(ctx context.Context) (*entity.FSMBlueprint, error)
	Save(ctx context.Context, b entity.FSMBlueprint) error
}
