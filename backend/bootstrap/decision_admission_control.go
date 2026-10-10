package bootstrap

import (
	"context"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math"
	"time"
)

func lockCutover(tx *gorm.DB) (*magi.DecisionWriterContractModel, error) {
	var gate magi.DecisionWriterContractModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=1").First(&gate).Error; err != nil {
		return nil, port.ErrDecisionWriterBlocked
	}
	return &gate, nil
}

func nextCutover(gate *magi.DecisionWriterContractModel) error {
	if gate.CutoverEpoch == math.MaxUint64 {
		return port.ErrCutoverPrecondition
	}
	gate.CutoverEpoch++
	gate.UpdatedAt = time.Now()
	return nil
}

// BlockDecisionWriter linearizes against Admit/Claim shared locks. Existing
// owners may drain; this does not revoke their T3/T4 credentials.
func BlockDecisionWriter(ctx context.Context, db *gorm.DB) (uint64, error) {
	var epoch uint64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gate, err := lockCutover(tx)
		if err != nil {
			return err
		}
		if err = nextCutover(gate); err != nil {
			return err
		}
		gate.AdmissionState = magi.DecisionAdmissionBlocked
		epoch = gate.CutoverEpoch
		return tx.Save(gate).Error
	})
	return epoch, err
}
