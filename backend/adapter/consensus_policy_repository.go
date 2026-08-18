package magi

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/port"
)

const consensusPolicyKey = "default"

// ConsensusPolicyModel stores the editable consensus/voting rules.
type ConsensusPolicyModel struct {
	Key     string `gorm:"primaryKey"`
	Content string `gorm:"type:text"`
}

func (ConsensusPolicyModel) TableName() string { return "consensus_policy" }

type consensusPolicyRepo struct{ db *gorm.DB }

func NewConsensusPolicyRepository(db *gorm.DB) port.ConsensusPolicyRepository {
	return &consensusPolicyRepo{db: db}
}

func (r *consensusPolicyRepo) Get(ctx context.Context) (*consensus.ConsensusPolicy, error) {
	var model ConsensusPolicyModel
	if err := r.db.WithContext(ctx).Where("key = ?", consensusPolicyKey).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	var policy consensus.ConsensusPolicy
	if err := json.Unmarshal([]byte(model.Content), &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (r *consensusPolicyRepo) Save(ctx context.Context, p consensus.ConsensusPolicy) error {
	content, err := json.Marshal(p)
	if err != nil {
		return err
	}
	model := ConsensusPolicyModel{Key: consensusPolicyKey, Content: string(content)}
	return r.db.WithContext(ctx).Save(&model).Error
}

var _ port.ConsensusPolicyRepository = (*consensusPolicyRepo)(nil)
