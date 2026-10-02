package bootstrap

import (
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/gorm"
)

const preS27ArtifactDDL = `
CREATE TABLE magi_agent_run (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE evidence_record (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE claim (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE magi_vote (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE debate_round (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE reflection (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    agent_run_id VARCHAR(191) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE magi_tool_call (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    agent_run_id VARCHAR(191) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE resolution (
    id VARCHAR(191) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE magi_agent_checkpoint (
    run_id VARCHAR(191) NOT NULL PRIMARY KEY
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`

func seedPreS27Artifacts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range splitS27SQLStatements(preS27ArtifactDDL) {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed pre-S27 schema: %v\n%s", err, statement)
		}
	}
	if err := db.Exec(`INSERT INTO magi_agent_run (id, case_id) VALUES ('run-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy agent run: %v", err)
	}
	if err := db.Exec(`INSERT INTO evidence_record (id, case_id) VALUES ('ev-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy evidence: %v", err)
	}
	if err := db.Exec(`INSERT INTO claim (id, case_id) VALUES ('cl-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy claim: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_vote (id, case_id) VALUES ('vote-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy vote: %v", err)
	}
	if err := db.Exec(`INSERT INTO debate_round (id, case_id) VALUES ('deb-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy debate: %v", err)
	}
	if err := db.Exec(`INSERT INTO reflection (id, agent_run_id) VALUES ('refl-legacy', 'run-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy reflection: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_tool_call (id, agent_run_id) VALUES ('tool-legacy', 'run-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy tool call: %v", err)
	}
	if err := db.Exec(`INSERT INTO resolution (id, case_id) VALUES ('res-legacy', 'case-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy resolution: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_agent_checkpoint (run_id) VALUES ('checkpoint-legacy')`).Error; err != nil {
		t.Fatalf("seed legacy checkpoint: %v", err)
	}
}

func splitS27SQLStatements(script string) []string {
	var out []string
	for _, part := range strings.Split(script, ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func assertS27GenerationColumn(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var row struct {
		DataType   string  `gorm:"column:data_type"`
		IsNullable string  `gorm:"column:is_nullable"`
		DefaultVal *string `gorm:"column:column_default"`
	}
	if err := db.Raw(`SELECT DATA_TYPE AS data_type, IS_NULLABLE AS is_nullable,
			COLUMN_DEFAULT AS column_default
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'execution_generation'`, table).
		Scan(&row).Error; err != nil {
		t.Fatalf("read %s.execution_generation: %v", table, err)
	}
	if row.DataType != "bigint" || row.IsNullable != "NO" || row.DefaultVal == nil || *row.DefaultVal != "0" {
		t.Fatalf("%s.execution_generation shape = %+v, want BIGINT NOT NULL DEFAULT 0", table, row)
	}
}

func checkpointPrimaryKeyColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var rows []struct {
		ColumnName string `gorm:"column:column_name"`
	}
	if err := db.Raw(`SELECT COLUMN_NAME AS column_name
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_agent_checkpoint'
		  AND INDEX_NAME = 'PRIMARY'
		ORDER BY SEQ_IN_INDEX`).Scan(&rows).Error; err != nil {
		t.Fatalf("read checkpoint primary key: %v", err)
	}
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].ColumnName
	}
	return out
}

func TestMySQLMigration_S27ScopesArtifactsAndCheckpointsByGeneration(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := openSchema(t, dsn)
	seedPreS27Artifacts(t, db)
	applyMySQLScript(t, db, s27MigrationPath)

	tables := []string{
		"magi_agent_run", "evidence_record", "claim", "magi_vote",
		"debate_round", "reflection", "magi_tool_call", "resolution",
		"magi_agent_checkpoint",
	}
	for _, table := range tables {
		assertS27GenerationColumn(t, db, table)
	}

	for _, table := range []string{"magi_agent_run", "evidence_record", "claim", "magi_vote", "debate_round", "reflection", "magi_tool_call", "resolution", "magi_agent_checkpoint"} {
		var positives int64
		if err := db.Raw("SELECT COUNT(*) FROM " + table + " WHERE execution_generation <> 0").Scan(&positives).Error; err != nil {
			t.Fatalf("count legacy generations in %s: %v", table, err)
		}
		if positives != 0 {
			t.Fatalf("%s contains %d legacy rows with fabricated positive generation", table, positives)
		}
	}

	for _, table := range []string{"reflection", "magi_tool_call"} {
		var caseID string
		if err := db.Raw("SELECT case_id FROM " + table + " LIMIT 1").Scan(&caseID).Error; err != nil {
			t.Fatalf("read %s.case_id: %v", table, err)
		}
		if caseID != "case-legacy" {
			t.Fatalf("%s legacy case_id = %q, want relational backfill case-legacy", table, caseID)
		}
	}

	pk := checkpointPrimaryKeyColumns(t, db)
	if len(pk) != 2 || pk[0] != "run_id" || pk[1] != "execution_generation" {
		t.Fatalf("checkpoint primary key = %v, want [run_id execution_generation]", pk)
	}

	// Startup AutoMigrate over the upgraded schema must preserve the same
	// generation/key contract.
	upgraded := provideDBForTest(t, dsn)
	for _, table := range tables {
		assertS27GenerationColumn(t, upgraded, table)
	}
	pk = checkpointPrimaryKeyColumns(t, upgraded)
	if len(pk) != 2 || pk[0] != "run_id" || pk[1] != "execution_generation" {
		t.Fatalf("upgraded AutoMigrate checkpoint primary key = %v", pk)
	}

	// A fresh schema created only by AutoMigrate reaches the same contract.
	fresh := provideDBForTest(t, newMySQLSchema(t))
	for _, table := range tables {
		assertS27GenerationColumn(t, fresh, table)
	}
	pk = checkpointPrimaryKeyColumns(t, fresh)
	if len(pk) != 2 || pk[0] != "run_id" || pk[1] != "execution_generation" {
		t.Fatalf("fresh checkpoint primary key = %v", pk)
	}

	if !fresh.Migrator().HasColumn(&magi.ReflectionModel{}, "case_id") ||
		!fresh.Migrator().HasColumn(&magi.ToolCallModel{}, "case_id") {
		t.Fatal("fresh schema must persist direct case provenance for reflection/tool calls")
	}
}
