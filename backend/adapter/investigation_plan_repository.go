package magi

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// InvestigationPlanModel persists the case investigation plan.
type InvestigationPlanModel struct {
	CaseID    string `gorm:"primaryKey"`
	ItemsJSON string `gorm:"type:mediumtext"`
	UpdatedAt time.Time
}

func (InvestigationPlanModel) TableName() string { return "investigation_plan" }

type investigationPlanRepo struct{ db *gorm.DB }

func NewInvestigationPlanRepository(db *gorm.DB) port.InvestigationPlanRepository {
	return &investigationPlanRepo{db: db}
}

func (r *investigationPlanRepo) Save(ctx context.Context, p *entity.InvestigationPlan) error {
	items, err := json.Marshal(p.Items)
	if err != nil {
		return err
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
	model := InvestigationPlanModel{CaseID: p.CaseID, ItemsJSON: string(items), UpdatedAt: p.UpdatedAt}
	return r.db.WithContext(ctx).Save(&model).Error
}

func (r *investigationPlanRepo) Get(ctx context.Context, caseID string) (*entity.InvestigationPlan, error) {
	var model InvestigationPlanModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var items []entity.InvestigationPlanItem
	if err := json.Unmarshal([]byte(model.ItemsJSON), &items); err != nil {
		return nil, err
	}
	return &entity.InvestigationPlan{CaseID: model.CaseID, Items: items, UpdatedAt: model.UpdatedAt}, nil
}

var _ port.InvestigationPlanRepository = (*investigationPlanRepo)(nil)
