package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// GoldenCaseModel persists online-golden decision cases.
type GoldenCaseModel struct {
	ID               string `gorm:"primaryKey"`
	CaseID           string `gorm:"index"`
	Question         string `gorm:"type:text"`
	Context          string `gorm:"type:text"`
	ExpectedDecision string
	CreatedAt        time.Time
}

func (GoldenCaseModel) TableName() string { return "golden_case" }

type goldenRepo struct{ db *gorm.DB }

func NewGoldenRepository(db *gorm.DB) port.GoldenRepository {
	return &goldenRepo{db: db}
}

func (r *goldenRepo) Create(ctx context.Context, g *entity.GoldenCase) error {
	return r.db.WithContext(ctx).Create(&GoldenCaseModel{
		ID: g.ID, CaseID: g.CaseID, Question: g.Question, Context: g.Context,
		ExpectedDecision: string(g.ExpectedDecision), CreatedAt: g.CreatedAt,
	}).Error
}

func (r *goldenRepo) List(ctx context.Context) ([]*entity.GoldenCase, error) {
	var models []GoldenCaseModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.GoldenCase, 0, len(models))
	for i := range models {
		m := &models[i]
		out = append(out, &entity.GoldenCase{
			ID: m.ID, CaseID: m.CaseID, Question: m.Question, Context: m.Context,
			ExpectedDecision: entity.VoteDecision(m.ExpectedDecision), CreatedAt: m.CreatedAt,
		})
	}
	return out, nil
}

func (r *goldenRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&GoldenCaseModel{}).Error
}

var _ port.GoldenRepository = (*goldenRepo)(nil)
