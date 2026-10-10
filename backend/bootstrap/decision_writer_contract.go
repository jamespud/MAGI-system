package bootstrap

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

type writerColumn struct {
	Name     string  `gorm:"column:column_name"`
	Type     string  `gorm:"column:data_type"`
	FullType string  `gorm:"column:column_type"`
	Nullable string  `gorm:"column:is_nullable"`
	Default  *string `gorm:"column:column_default"`
	Length   *int64  `gorm:"column:character_maximum_length"`
}

func writerSchemaError(detail string) error {
	return fmt.Errorf("%w: %s", port.ErrDecisionWriterSchema, detail)
}

// VerifyDecisionWriterSchema is read-only. It never repairs a column, index,
// cursor or historical provenance and is safe to run while admission is blocked.
func VerifyDecisionWriterSchema(ctx context.Context, db *gorm.DB) error {
	return verifyDecisionWriterSchema(ctx, db, true)
}

// VerifyDecisionGenerationSchema checks the established T1-T4 contract before
// installing S29. It does not mutate legacy provenance or infer missing stages.
func VerifyDecisionGenerationSchema(ctx context.Context, db *gorm.DB) error {
	return verifyDecisionWriterSchema(ctx, db, false)
}
func verifyDecisionWriterSchema(ctx context.Context, db *gorm.DB, control bool) error {
	if db == nil || db.Dialector.Name() != "mysql" {
		return writerSchemaError("MySQL is required for writer mode")
	}
	db = db.WithContext(ctx)
	tables := []string{"decision_case", "decision_job", "decision_job_claim", "magi_agent_run", "evidence_record", "claim", "magi_vote", "debate_round", "reflection", "magi_tool_call", "magi_agent_checkpoint", "resolution", "magi_event", "magi_event_cursor", "decision_writer_contract"}
	if !control {
		tables = tables[:len(tables)-1]
	}
	columns := make(map[string]map[string]writerColumn, len(tables))
	for _, table := range tables {
		var engine string
		if err := db.Raw("SELECT ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table).Scan(&engine).Error; err != nil {
			return writerSchemaError(err.Error())
		}
		if engine != "InnoDB" {
			return writerSchemaError(table + " must use InnoDB")
		}
		var rows []writerColumn
		if err := db.Raw(`SELECT COLUMN_NAME AS column_name, DATA_TYPE AS data_type, COLUMN_TYPE AS column_type, IS_NULLABLE AS is_nullable, COLUMN_DEFAULT AS column_default, CHARACTER_MAXIMUM_LENGTH AS character_maximum_length FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, table).Scan(&rows).Error; err != nil {
			return writerSchemaError(err.Error())
		}
		columns[table] = map[string]writerColumn{}
		for _, row := range rows {
			columns[table][row.Name] = row
		}
		if len(rows) == 0 {
			return writerSchemaError("missing table " + table)
		}
	}
	check := func(table, name, typ, nullable string, def *string, minLength int64) error {
		c, ok := columns[table][name]
		if !ok || c.Type != typ || c.Nullable != nullable || (minLength > 0 && (c.Length == nil || *c.Length < minLength)) {
			return writerSchemaError(table + "." + name + " has missing/incompatible type, nullability or length")
		}
		if def != nil && (c.Default == nil || *c.Default != *def) {
			return writerSchemaError(table + "." + name + " has incompatible default")
		}
		return nil
	}
	zero, empty := "0", ""
	for _, table := range tables[:13] {
		def := &zero
		if table == "decision_job_claim" {
			def = nil
		}
		if err := check(table, "execution_generation", "bigint", "NO", def, 0); err != nil {
			return err
		}
		if strings.Contains(columns[table]["execution_generation"].FullType, "unsigned") {
			return writerSchemaError(table + " generation must be signed BIGINT")
		}
	}
	for _, table := range []string{"reflection", "magi_tool_call", "magi_agent_checkpoint"} {
		if err := check(table, "case_id", "varchar", "NO", &empty, 64); err != nil {
			return err
		}
	}
	if err := check("decision_job", "claim_token", "varchar", "YES", nil, 36); err != nil {
		return err
	}
	for _, c := range []struct {
		name, typ string
		length    int64
	}{
		{"claim_token", "varchar", 36}, {"job_id", "varchar", 64}, {"case_id", "varchar", 64}, {"worker_id", "varchar", 255}, {"attempt", "bigint", 0}, {"max_attempts", "bigint", 0}, {"lease_until", "datetime", 0}, {"claimed_at", "datetime", 0},
	} {
		if err := check("decision_job_claim", c.name, c.typ, "NO", nil, c.length); err != nil {
			return err
		}
	}
	if control {
		for _, c := range []struct {
			name, typ string
			length    int64
		}{
			{"id", "tinyint", 0}, {"contract_version", "int", 0}, {"admission_state", "varchar", 16}, {"cutover_epoch", "bigint", 0}, {"legacy_rollback_forbidden", "tinyint", 0}, {"writer_account", "varchar", 255}, {"legacy_account", "varchar", 255}, {"updated_at", "datetime", 0},
		} {
			if err := check("decision_writer_contract", c.name, c.typ, "NO", nil, c.length); err != nil {
				return err
			}
		}
		for _, name := range []string{"id", "contract_version", "cutover_epoch"} {
			if !strings.Contains(columns["decision_writer_contract"][name].FullType, "unsigned") {
				return writerSchemaError("writer control " + name + " must be unsigned")
			}
		}
		for name, expected := range map[string]string{"contract_version": "1", "cutover_epoch": "0", "legacy_rollback_forbidden": "0", "writer_account": "", "legacy_account": ""} {
			if err := check("decision_writer_contract", name, columns["decision_writer_contract"][name].Type, "NO", &expected, 0); err != nil {
				return err
			}
		}
		blocked := "BLOCKED"
		if err := check("decision_writer_contract", "admission_state", "varchar", "NO", &blocked, 16); err != nil {
			return err
		}
	}
	if err := check("magi_event", "seq", "bigint", "NO", nil, 0); err != nil {
		return err
	}
	if !strings.Contains(columns["magi_event"]["seq"].FullType, "unsigned") {
		return writerSchemaError("magi_event.seq must be unsigned BIGINT")
	}
	if err := check("magi_event_cursor", "case_id", "varchar", "NO", nil, 64); err != nil {
		return err
	}
	if err := check("magi_event_cursor", "next_seq", "bigint", "NO", nil, 0); err != nil {
		return err
	}
	for _, spec := range []struct {
		table           string
		cols            []string
		primary, unique bool
	}{
		{"decision_case", []string{"id"}, true, true}, {"decision_job", []string{"id"}, true, true}, {"resolution", []string{"case_id"}, false, true},
		{"decision_job", []string{"case_id"}, false, true}, {"decision_job", []string{"claim_token"}, false, true},
		{"decision_job_claim", []string{"claim_token"}, true, true}, {"decision_job_claim", []string{"job_id"}, false, false}, {"decision_job_claim", []string{"case_id", "execution_generation"}, false, false},
		{"magi_agent_checkpoint", []string{"run_id", "execution_generation"}, true, true}, {"magi_agent_checkpoint", []string{"case_id", "execution_generation"}, false, false},
		{"magi_event", []string{"case_id", "seq"}, false, true}, {"magi_event_cursor", []string{"case_id"}, true, true}, {"decision_writer_contract", []string{"id"}, true, true},
	} {
		if spec.table == "decision_writer_contract" && !control {
			continue
		}
		if err := verifyWriterIndex(db, spec.table, spec.cols, spec.primary, spec.unique); err != nil {
			return err
		}
	}
	for _, table := range tables[3:10] {
		if err := verifyWriterIndex(db, table, []string{"case_id", "execution_generation"}, false, false); err != nil {
			return err
		}
	}
	if err := verifyEventSequenceContract(db); err != nil {
		return writerSchemaError(err.Error())
	}
	if !control {
		return nil
	}
	var count int64
	if err := db.Model(&magi.DecisionWriterContractModel{}).Count(&count).Error; err != nil || count != 1 {
		return writerSchemaError("writer contract must contain exactly one singleton")
	}
	var gate magi.DecisionWriterContractModel
	if err := db.Where("id=1").First(&gate).Error; err != nil {
		return writerSchemaError("missing singleton writer contract")
	}
	if gate.ContractVersion != port.DecisionWriterContractVersion {
		return port.ErrDecisionWriterVersion
	}
	if gate.AdmissionState != magi.DecisionAdmissionBlocked && gate.AdmissionState != magi.DecisionAdmissionEnabled {
		return writerSchemaError("invalid admission_state")
	}
	return nil
}

func verifyWriterIndex(db *gorm.DB, table string, cols []string, primary, unique bool) error {
	var rows []struct {
		Name      string `gorm:"column:index_name"`
		NonUnique int    `gorm:"column:non_unique"`
		Column    string `gorm:"column:column_name"`
		Prefix    *int   `gorm:"column:sub_part"`
	}
	if err := db.Raw(`SELECT INDEX_NAME AS index_name, NON_UNIQUE AS non_unique, COLUMN_NAME AS column_name, SUB_PART AS sub_part FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table).Scan(&rows).Error; err != nil {
		return writerSchemaError(err.Error())
	}
	groups := map[string][]string{}
	invalid := map[string]bool{}
	for _, r := range rows {
		groups[r.Name] = append(groups[r.Name], r.Column)
		if r.Prefix != nil || (primary && r.Name != "PRIMARY") || (unique && r.NonUnique != 0) {
			invalid[r.Name] = true
		}
	}
	for name, actual := range groups {
		if !invalid[name] && reflect.DeepEqual(actual, cols) {
			return nil
		}
	}
	return writerSchemaError(fmt.Sprintf("%s missing ordered index %v (primary=%v unique=%v)", table, cols, primary, unique))
}
