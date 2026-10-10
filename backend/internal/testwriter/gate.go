// Package testwriter prepares explicit enabled fixtures. Production assembly
// never imports it and never has an admission bypass.
package testwriter

import (
	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"testing"
)

func Enable(t testing.TB, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&magi.DecisionWriterContractModel{}); err != nil {
		t.Fatal(err)
	}
	row := magi.DecisionWriterContractModel{ID: 1, ContractVersion: 1, AdmissionState: magi.DecisionAdmissionEnabled, CutoverEpoch: 1, LegacyRollbackForbidden: true}
	if db.Dialector.Name() == "mysql" {
		if err := db.Raw("SELECT CURRENT_USER()").Scan(&row.WriterAccount).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}
