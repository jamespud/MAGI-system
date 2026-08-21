package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type selfImproveRepo struct{ db *gorm.DB }

func NewSelfImproveRepository(db *gorm.DB) *selfImproveRepo {
	return &selfImproveRepo{db: db}
}

func (r *selfImproveRepo) Create(ctx context.Context, s *entity.SelfImproveSuggestion) error {
	return r.db.WithContext(ctx).Create(selfImproveToModel(s)).Error
}

func (r *selfImproveRepo) List(ctx context.Context) ([]*entity.SelfImproveSuggestion, error) {
	var models []SelfImproveModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.SelfImproveSuggestion, 0, len(models))
	for i := range models {
		out = append(out, selfImproveFromModel(&models[i]))
	}
	return out, nil
}

func (r *selfImproveRepo) Get(ctx context.Context, id string) (*entity.SelfImproveSuggestion, error) {
	var model SelfImproveModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return selfImproveFromModel(&model), nil
}

func (r *selfImproveRepo) UpdateStatus(ctx context.Context, id, status string) error {
	updates := map[string]any{"status": status, "updated_at": time.Now()}
	if status == entity.SelfImproveApplied {
		now := time.Now()
		updates["applied_at"] = &now
	}
	return r.db.WithContext(ctx).Model(&SelfImproveModel{}).Where("id = ?", id).Updates(updates).Error
}

func (r *selfImproveRepo) UpdateRule(ctx context.Context, id, rule string) error {
	updates := map[string]any{"suggested_rule": rule, "status": entity.SelfImproveOpen, "updated_at": time.Now()}
	return r.db.WithContext(ctx).Model(&SelfImproveModel{}).Where("id = ?", id).Updates(updates).Error
}

func selfImproveToModel(s *entity.SelfImproveSuggestion) *SelfImproveModel {
	return &SelfImproveModel{
		ID: s.ID, CaseID: s.CaseID, RunID: s.RunID, AgentCode: s.AgentCode,
		Category: s.Category, Failure: s.Failure, Summary: s.Summary,
		SuggestedRule: s.SuggestedRule, PromptKey: s.PromptKey, PromptContent: s.PromptContent,
		Status: s.Status, CreatedAt: s.CreatedAt, AppliedAt: s.AppliedAt,
	}
}

func selfImproveFromModel(m *SelfImproveModel) *entity.SelfImproveSuggestion {
	return &entity.SelfImproveSuggestion{
		ID: m.ID, CaseID: m.CaseID, RunID: m.RunID, AgentCode: m.AgentCode,
		Category: m.Category, Failure: m.Failure, Summary: m.Summary,
		SuggestedRule: m.SuggestedRule, PromptKey: m.PromptKey, PromptContent: m.PromptContent,
		Status: m.Status, CreatedAt: m.CreatedAt, AppliedAt: m.AppliedAt,
	}
}

var _ port.SelfImproveRepository = (*selfImproveRepo)(nil)
