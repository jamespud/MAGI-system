package bootstrap

import (
	"context"
	"errors"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

func t6SchemaFixture(t *testing.T) (string, *gorm.DB) {
	t.Helper()
	dsn := newMySQLSchema(t)
	db := openSchema(t, dsn)
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.DecisionWriterContractModel{ID: 1, AdmissionState: magi.DecisionAdmissionEnabled, CutoverEpoch: 1, LegacyRollbackForbidden: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE decision_writer_contract SET writer_account=CURRENT_USER() WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	return dsn, db
}

func TestMySQLDecisionWriter_RuntimeNeverRepairsSchema(t *testing.T) {
	defects := []struct{ name, sql string }{
		{"nontransactional_case", "ALTER TABLE decision_case ENGINE=MyISAM"},
		{"S26_wrong_token_type", "ALTER TABLE decision_job MODIFY claim_token CHAR(36) NULL"},
		{"S26_nullable_registry_lease", "ALTER TABLE decision_job_claim MODIFY lease_until DATETIME(3) NULL"},
		{"S27_missing_checkpoint_index", "ALTER TABLE magi_agent_checkpoint DROP INDEX idx_checkpoint_case_generation"},
		{"S29_wrong_control_default", "ALTER TABLE decision_writer_contract ALTER admission_state SET DEFAULT 'ENABLED'"},
		{"S29_signed_epoch", "ALTER TABLE decision_writer_contract MODIFY cutover_epoch BIGINT NOT NULL DEFAULT 0"},
		{"S25_missing_generation", "ALTER TABLE decision_case DROP COLUMN execution_generation"},
		{"S25_nullable_generation", "ALTER TABLE decision_job MODIFY execution_generation BIGINT NULL DEFAULT 0"},
		{"S25_wrong_default", "ALTER TABLE decision_case ALTER execution_generation SET DEFAULT 1"},
		{"S26_missing_claim_unique", "ALTER TABLE decision_job DROP INDEX uk_decision_job_claim_token"},
		{"S26_missing_registry", "DROP TABLE decision_job_claim"},
		{"S27_missing_artifact_generation", "ALTER TABLE magi_vote DROP COLUMN execution_generation"},
		{"S27_missing_reflection_case", "ALTER TABLE reflection DROP COLUMN case_id"},
		{"S27_missing_tool_case", "ALTER TABLE magi_tool_call DROP COLUMN case_id"},
		{"S27_wrong_checkpoint_key", "ALTER TABLE magi_agent_checkpoint DROP PRIMARY KEY, ADD PRIMARY KEY (execution_generation, run_id)"},
		{"S28_missing_event_generation", "ALTER TABLE magi_event DROP COLUMN execution_generation"},
		{"S16_cursor_data_corruption", "INSERT INTO magi_event (id,case_id,type,seq,timestamp) VALUES ('broken-event','historical-case','case.completed',1,CURRENT_TIMESTAMP);"},
		{"S16_nullable_seq", "ALTER TABLE magi_event MODIFY seq BIGINT UNSIGNED NULL"},
		{"S16_missing_unique", "ALTER TABLE magi_event DROP INDEX idx_event_case_seq"},
	}
	for _, defect := range defects {
		t.Run(defect.name, func(t *testing.T) {
			dsn, db := t6SchemaFixture(t)
			if err := db.Exec(defect.sql).Error; err != nil {
				t.Fatal(err)
			}
			before := schemaFingerprint(t, db)
			cfg := &Config{}
			cfg.Database.DSN, cfg.Database.LogLevel = dsn, "silent"
			started := false
			app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(provideDB), fx.Invoke(func(*gorm.DB) { started = true }))
			if app.Err() == nil {
				t.Error("runtime accepted incompatible schema")
			}
			if !errors.Is(app.Err(), port.ErrDecisionWriterSchema) {
				t.Errorf("expected typed schema error: %v", app.Err())
			}
			if started {
				t.Error("startup dependents ran before capability verification")
			}
			if after := schemaFingerprint(t, db); before != after {
				t.Error("runtime silently repaired incompatible schema")
			}
		})
	}
}

func TestMySQLDecisionWriter_CompatibleRuntimeIsReadOnly(t *testing.T) {
	dsn, db := t6SchemaFixture(t)
	before := schemaFingerprint(t, db)
	for range 2 {
		if err := VerifyDecisionWriterSchema(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{}
	cfg.Database.DSN, cfg.Database.LogLevel = dsn, "silent"
	writer, err := provideDB(cfg)
	if err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
	pool, _ := writer.DB()
	t.Cleanup(func() { _ = pool.Close() })
	if after := schemaFingerprint(t, db); before != after {
		t.Fatal("compatible startup changed schema")
	}
}
