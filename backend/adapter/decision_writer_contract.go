package magi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DecisionAdmissionBlocked = "BLOCKED"
	DecisionAdmissionEnabled = "ENABLED"
)

// The singleton is managed with a separate migrator identity. Runtime writers
// receive SELECT only on this table, so they cannot open their own admission.
type DecisionWriterContractModel struct {
	ID                      uint8     `gorm:"primaryKey;type:tinyint unsigned;autoIncrement:false"`
	ContractVersion         uint32    `gorm:"type:int unsigned;not null;default:1"`
	AdmissionState          string    `gorm:"size:16;not null;default:BLOCKED"`
	CutoverEpoch            uint64    `gorm:"type:bigint unsigned;not null;default:0"`
	LegacyRollbackForbidden bool      `gorm:"not null;default:false"`
	WriterAccount           string    `gorm:"size:255;not null;default:''"`
	LegacyAccount           string    `gorm:"size:255;not null;default:''"`
	UpdatedAt               time.Time `gorm:"type:datetime;not null;default:CURRENT_TIMESTAMP"`
}

func (DecisionWriterContractModel) TableName() string { return "decision_writer_contract" }

func lockDecisionWriterGate(tx *gorm.DB) error {
	var gate DecisionWriterContractModel
	q := tx.Where("id = ?", 1)
	if tx.Dialector.Name() == "mysql" {
		q = q.Clauses(clause.Locking{Strength: "SHARE"})
	}
	if err := q.First(&gate).Error; err != nil {
		return fmt.Errorf("%w: gate unavailable: %v", port.ErrDecisionWriterBlocked, err)
	}
	if gate.ContractVersion != port.DecisionWriterContractVersion {
		return port.ErrDecisionWriterVersion
	}
	if gate.AdmissionState != DecisionAdmissionEnabled || !gate.LegacyRollbackForbidden || gate.CutoverEpoch == 0 {
		return port.ErrDecisionWriterBlocked
	}
	if tx.Dialector.Name() == "mysql" {
		var account string
		if err := tx.Raw("SELECT CURRENT_USER()").Scan(&account).Error; err != nil {
			return err
		}
		if gate.WriterAccount == "" || gate.WriterAccount != account {
			return port.ErrDecisionWriterIdentity
		}
	}
	return nil
}

type decisionWriterGuard struct{ db *gorm.DB }

func NewDecisionWriterGuard(db *gorm.DB) port.DecisionWriterGuard {
	return &decisionWriterGuard{db: db}
}
func (g *decisionWriterGuard) CheckDecisionWriter(ctx context.Context) error {
	return g.db.WithContext(ctx).Transaction(lockDecisionWriterGate)
}

// IsDecisionWriterDenied distinguishes admission denial from an uncertain COMMIT.
func IsDecisionWriterDenied(err error) bool {
	return errors.Is(err, port.ErrDecisionWriterBlocked) || errors.Is(err, port.ErrDecisionWriterVersion) || errors.Is(err, port.ErrDecisionWriterIdentity) || errors.Is(err, port.ErrDecisionWriterSchema)
}
