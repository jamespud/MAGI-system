package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// JudgeRepository persists LLM-as-a-Judge evaluations for completed cases.
type JudgeRepository interface {
	Save(ctx context.Context, r *entity.JudgeResult) error
	GetLatest(ctx context.Context, caseID string) (*entity.JudgeResult, error)
}
