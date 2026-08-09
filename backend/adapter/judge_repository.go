package magi

import (
	"context"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

type judgeRepo struct {
	db *gorm.DB
}

// NewJudgeRepository returns the GORM-backed JudgeRepository.
func NewJudgeRepository(db *gorm.DB) port.JudgeRepository {
	return &judgeRepo{db: db}
}

func (r *judgeRepo) Save(ctx context.Context, j *entity.JudgeResult) error {
	if j == nil {
		return nil
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now()
	}
	m := JudgeModel{
		CaseID: j.CaseID, ReportQuality: j.ReportQuality, EvidenceConsistency: j.EvidenceConsistency,
		ReflectionValidity: j.ReflectionValidity, Overall: j.Overall, Rationale: j.Rationale,
		ModelName: j.ModelName, CreatedAt: j.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *judgeRepo) GetLatest(ctx context.Context, caseID string) (*entity.JudgeResult, error) {
	var m JudgeModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Order("created_at DESC").First(&m).Error; err != nil {
		return nil, err
	}
	return &entity.JudgeResult{
		CaseID: m.CaseID, ReportQuality: m.ReportQuality, EvidenceConsistency: m.EvidenceConsistency,
		ReflectionValidity: m.ReflectionValidity, Overall: m.Overall, Rationale: m.Rationale,
		ModelName: m.ModelName, CreatedAt: m.CreatedAt,
	}, nil
}

var _ port.JudgeRepository = (*judgeRepo)(nil)
