package magi

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// RolePolicyModel stores the editable role-contract specification.
type RolePolicyModel struct {
	Code    string `gorm:"primaryKey"`
	Content string `gorm:"type:mediumtext"`
}

func (RolePolicyModel) TableName() string { return "role_policy" }

type rolePolicyRepo struct{ db *gorm.DB }

func NewRolePolicyRepository(db *gorm.DB) port.RolePolicyRepository {
	return &rolePolicyRepo{db: db}
}

func (r *rolePolicyRepo) Get(ctx context.Context, code string) (*entity.RolePolicy, error) {
	var model RolePolicyModel
	if err := r.db.WithContext(ctx).Where("code = ?", code).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var policy entity.RolePolicy
	if err := json.Unmarshal([]byte(model.Content), &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (r *rolePolicyRepo) Save(ctx context.Context, code string, p entity.RolePolicy) error {
	content, err := json.Marshal(p)
	if err != nil {
		return err
	}
	model := RolePolicyModel{Code: code, Content: string(content)}
	return r.db.WithContext(ctx).Save(&model).Error
}

var _ port.RolePolicyRepository = (*rolePolicyRepo)(nil)
