package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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
	// A legacy checkpoint whose RunID happens to equal a magi_agent_run primary
	// key. Pre-S27 checkpoints have no Case column at all, so this coincidence
	// must not be read as provenance by the migration.
	if err := db.Exec(`INSERT INTO magi_agent_checkpoint (run_id) VALUES ('run-legacy')`).Error; err != nil {
		t.Fatalf("seed RunID-colliding legacy checkpoint: %v", err)
	}
	// Pre-existing orphan history: no magi_agent_run row ever recorded.
	if err := db.Exec(`INSERT INTO reflection (id, agent_run_id) VALUES ('refl-orphan', 'run-missing')`).Error; err != nil {
		t.Fatalf("seed orphan reflection: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_tool_call (id, agent_run_id) VALUES ('tool-orphan', 'run-missing')`).Error; err != nil {
		t.Fatalf("seed orphan tool call: %v", err)
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

	// Relational backfill: the linked legacy row takes the Case of its AgentRun.
	for _, tc := range []struct{ table, id string }{
		{"reflection", "refl-legacy"},
		{"magi_tool_call", "tool-legacy"},
	} {
		var caseID string
		if err := db.Raw("SELECT case_id FROM "+tc.table+" WHERE id = ?", tc.id).Scan(&caseID).Error; err != nil {
			t.Fatalf("read %s.case_id: %v", tc.table, err)
		}
		if caseID != "case-legacy" {
			t.Fatalf("%s %s case_id = %q, want relational backfill case-legacy", tc.table, tc.id, caseID)
		}
	}

	// Broken relationships and checkpoints keep unknown provenance rather than
	// failing the upgrade or being attributed to a Case by guesswork.
	for _, tc := range []struct{ table, id string }{
		{"reflection", "refl-orphan"},
		{"magi_tool_call", "tool-orphan"},
	} {
		var caseID string
		if err := db.Raw("SELECT case_id FROM "+tc.table+" WHERE id = ?", tc.id).Scan(&caseID).Error; err != nil {
			t.Fatalf("read orphan %s.case_id: %v", tc.table, err)
		}
		if caseID != "" {
			t.Fatalf("orphan %s %s case_id = %q, want unknown provenance", tc.table, tc.id, caseID)
		}
	}

	// Every legacy checkpoint keeps case_id empty, including the one whose RunID
	// collides with a magi_agent_run primary key: the pre-S27 table recorded no
	// Case, and RunID is a logical identity, not a reference.
	var checkpointCases []struct {
		RunID  string `gorm:"column:run_id"`
		CaseID string `gorm:"column:case_id"`
	}
	if err := db.Raw("SELECT run_id, case_id FROM magi_agent_checkpoint ORDER BY run_id").Scan(&checkpointCases).Error; err != nil {
		t.Fatalf("read checkpoint provenance: %v", err)
	}
	if len(checkpointCases) != 2 {
		t.Fatalf("legacy checkpoints = %d, want 2", len(checkpointCases))
	}
	for _, row := range checkpointCases {
		if row.CaseID != "" {
			t.Fatalf("legacy checkpoint %s case_id = %q, want unknown provenance", row.RunID, row.CaseID)
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

// T3 / S27 legacy checkpoints carry no recorded Case provenance: the pre-S27
// table stored only run_id and snapshot state, and run_id is the logical
// working-memory identity rather than a declared reference to magi_agent_run.
// This pins the downstream contract on real MySQL: the generation-scoped loader
// never serves a legacy row, Delete removes the rows it can attribute (direct
// case_id, or the pre-S27 run_id containment rule), and a legacy row that
// matches neither is retained as unknown-provenance history instead of being
// guessed into a Case.
func TestMySQLMigration_S27LegacyCheckpointRetention(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	const caseID = "case-s27-retention"
	repo, jobs, job := seedMySQLDecisionJob(t, db, caseID, 3)
	checkpoints, ok := repo.CheckpointRepo().(port.GenerationCheckpointRepository)
	if !ok {
		t.Fatal("checkpoint repository missing GenerationCheckpointRepository")
	}
	ctx := context.Background()
	worker := "worker-s27-retention"
	claimed, ok, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	owner := &entity.ExecutionContext{
		CaseID: claimed.CaseID, JobID: claimed.ID, WorkerID: worker,
		JobAttempt: claimed.Attempt, ExecutionGeneration: claimed.ExecutionGeneration,
	}

	// Two upgraded pre-S27 rows: one keeps the purely logical identity (matches
	// nothing), the other equals a magi_agent_run primary key of this Case and
	// is therefore inside the pre-S27 deletion containment rule.
	logicalRunID := caseID + "-melchior-r1-investigate"
	matchedRunID := "run-s27-matched"
	if err := db.Create(&magi.AgentRunModel{
		ID: matchedRunID, CaseID: caseID, ExecutionGeneration: owner.ExecutionGeneration,
		StartedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed matched agent run: %v", err)
	}
	for _, runID := range []string{logicalRunID, matchedRunID} {
		if err := db.Create(&magi.CheckpointModel{RunID: runID, ExecutionGeneration: 0, CaseID: ""}).Error; err != nil {
			t.Fatalf("seed legacy checkpoint %s: %v", runID, err)
		}
	}
	positiveRunID := caseID + "-casper-g1-r1-investigate"
	if err := checkpoints.SaveForExecution(ctx, owner, &entity.AgentState{
		RunID: positiveRunID, CaseID: caseID,
		ExecutionGeneration: owner.ExecutionGeneration, StepCount: 1, Phase: "gather",
	}); err != nil {
		t.Fatalf("save generation-scoped checkpoint: %v", err)
	}

	// The current API is generation-scoped and must not answer a legacy row,
	// even while the Case and its owner are alive.
	for _, runID := range []string{logicalRunID, matchedRunID} {
		if got, err := checkpoints.LoadForExecution(ctx, owner, runID); err != nil || got != nil {
			t.Fatalf("LoadForExecution(%s) = %+v err=%v, want empty", runID, got, err)
		}
		legacy, err := repo.CheckpointRepo().Load(ctx, runID)
		if err != nil || legacy == nil {
			t.Fatalf("legacy Load(%s) = %+v err=%v, want the generation-0 row", runID, legacy, err)
		}
		if legacy.CaseID != "" || legacy.ExecutionGeneration != 0 {
			t.Fatalf("legacy checkpoint %s provenance = case %q generation %d, want unknown",
				runID, legacy.CaseID, legacy.ExecutionGeneration)
		}
	}

	if err := repo.CaseRepo().Delete(ctx, caseID); err != nil {
		t.Fatalf("delete case: %v", err)
	}

	var positiveRows int64
	if err := db.Model(&magi.CheckpointModel{}).Where("run_id = ?", positiveRunID).Count(&positiveRows).Error; err != nil {
		t.Fatalf("count generation-scoped checkpoint: %v", err)
	}
	if positiveRows != 0 {
		t.Fatalf("generation-scoped checkpoint rows after delete = %d, want 0", positiveRows)
	}
	var matchedRows int64
	if err := db.Model(&magi.CheckpointModel{}).Where("run_id = ?", matchedRunID).Count(&matchedRows).Error; err != nil {
		t.Fatalf("count matched legacy checkpoint: %v", err)
	}
	if matchedRows != 0 {
		t.Fatalf("checkpoint matching a Case AgentRun survived the delete: %d", matchedRows)
	}
	var retained struct {
		CaseID     string `gorm:"column:case_id"`
		Generation int64  `gorm:"column:execution_generation"`
	}
	if err := db.Raw("SELECT case_id, execution_generation FROM magi_agent_checkpoint WHERE run_id = ?", logicalRunID).
		Scan(&retained).Error; err != nil {
		t.Fatalf("read retained legacy checkpoint: %v", err)
	}
	if retained.CaseID != "" || retained.Generation != 0 {
		t.Fatalf("retained legacy checkpoint provenance = case %q generation %d, want unknown",
			retained.CaseID, retained.Generation)
	}
	var runs int64
	if err := db.Model(&magi.AgentRunModel{}).Where("case_id = ?", caseID).Count(&runs).Error; err != nil {
		t.Fatalf("count agent runs: %v", err)
	}
	if runs != 0 {
		t.Fatalf("agent runs after delete = %d, want 0", runs)
	}
}
