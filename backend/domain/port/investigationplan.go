package port

import (
	"context"

	"github.com/jamespud/magi/backend/domain/entity"
)

// InvestigationPlanRepository persists the case investigation plan.
type InvestigationPlanRepository interface {
	Save(ctx context.Context, p *entity.InvestigationPlan) error
	Get(ctx context.Context, caseID string) (*entity.InvestigationPlan, error)
}
