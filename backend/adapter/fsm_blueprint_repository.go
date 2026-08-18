package magi

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

const fsmBlueprintKey = "default"

// FSMBlueprintModel stores the editable orchestration blueprint.
type FSMBlueprintModel struct {
	Key     string `gorm:"primaryKey"`
	Content string `gorm:"type:mediumtext"`
}

func (FSMBlueprintModel) TableName() string { return "fsm_blueprint" }

type fsmBlueprintRepo struct{ db *gorm.DB }

func NewFSMBlueprintRepository(db *gorm.DB) port.FSMBlueprintRepository {
	return &fsmBlueprintRepo{db: db}
}

func (r *fsmBlueprintRepo) Get(ctx context.Context) (*entity.FSMBlueprint, error) {
	var model FSMBlueprintModel
	if err := r.db.WithContext(ctx).Where("key = ?", fsmBlueprintKey).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var blueprint entity.FSMBlueprint
	if err := json.Unmarshal([]byte(model.Content), &blueprint); err != nil {
		return nil, err
	}
	return &blueprint, nil
}

func (r *fsmBlueprintRepo) Save(ctx context.Context, b entity.FSMBlueprint) error {
	content, err := json.Marshal(b)
	if err != nil {
		return err
	}
	model := FSMBlueprintModel{Key: fsmBlueprintKey, Content: string(content)}
	return r.db.WithContext(ctx).Save(&model).Error
}

var _ port.FSMBlueprintRepository = (*fsmBlueprintRepo)(nil)
