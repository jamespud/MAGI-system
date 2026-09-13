package bootstrap

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	gosqlmysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
)

// This file covers the production MySQL database lifecycle that the SQLite
// suite cannot prove: ALTER TABLE against a populated table, MySQL-specific
// schema introspection, repeated-startup idempotency, and the DELIMITER-scripted
// S16 operator migration/repair path.
//
// Every scenario provisions its own schema on the server named by
// MAGI_TEST_MYSQL_DSN (created from the DSN's credentials and dropped on
// cleanup), so scenarios cannot pollute each other or the base database. The
// whole file skips when the variable is unset.

const (
	s16MigrationPath = "../../docker/atlas/migrations/magi_s16_event_sequence.sql"
	s16ResumePath    = "../../docker/atlas/repair/magi_s16_event_sequence_resume.sql"
	s23MigrationPath = "../../docker/atlas/migrations/magi_s23_runtime_identity_length.sql"
)

// preS23RuntimeInvocationDDL is the runtime kernel shape as published in S21
// before the identity columns were widened: run_id and attempt_id were
// varchar(64), which is why every run on MySQL failed with error 1406.
const preS23RuntimeInvocationDDL = `
CREATE TABLE runtime_invocation (
    invocation_id VARCHAR(64) NOT NULL PRIMARY KEY,
    run_id VARCHAR(64) NOT NULL,
    step_id VARCHAR(64) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    logical_ordinal INT NOT NULL,
    status VARCHAR(32) NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    operation_name VARCHAR(128) NOT NULL DEFAULT '',
    retry_safety VARCHAR(32) NOT NULL,
    idempotency_key VARCHAR(128) NULL,
    input_digest VARCHAR(64) NOT NULL DEFAULT '',
    input_json MEDIUMTEXT NOT NULL,
    output_json MEDIUMTEXT NULL,
    error TEXT NULL,
    started_at DATETIME(3) NULL,
    completed_at DATETIME(3) NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_runtime_run_step (run_id, step_id),
    KEY idx_runtime_run_status (run_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const preS23RuntimeAttemptDDL = `
CREATE TABLE runtime_invocation_attempt (
    attempt_id VARCHAR(64) NOT NULL PRIMARY KEY,
    invocation_id VARCHAR(64) NOT NULL,
    attempt_no INT NOT NULL,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    started_at DATETIME(3) NULL,
    completed_at DATETIME(3) NULL,
    error TEXT NULL,
    UNIQUE KEY uk_runtime_invocation_attempt_no (invocation_id, attempt_no),
    KEY idx_runtime_attempt_invocation (invocation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func seedPreS23RuntimeTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{preS23RuntimeInvocationDDL, preS23RuntimeAttemptDDL} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("seed pre-S23 runtime tables: %v", err)
		}
	}
}

var mysqlSchemaSeq atomic.Int64

func requireMySQLBaseDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MAGI_TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration tests — a green run without it does NOT mean MySQL was verified")
	}
	return dsn
}

// newMySQLSchema provisions a dedicated empty schema for one scenario and
// returns a DSN pointing at it.
func newMySQLSchema(t *testing.T) string {
	t.Helper()
	cfg, err := gosqlmysql.ParseDSN(requireMySQLBaseDSN(t))
	if err != nil {
		t.Fatalf("parse MAGI_TEST_MYSQL_DSN: %v", err)
	}
	adminCfg := *cfg
	adminCfg.DBName = ""
	admin, err := gorm.Open(MysqlDialector(adminCfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	name := fmt.Sprintf("magi_ci_%s_%d", shortSchemaSuffix(t.Name()), mysqlSchemaSeq.Add(1))
	if err := admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatalf("create schema %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error
		if sqlDB, dbErr := admin.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	cfg.DBName = name
	return cfg.FormatDSN()
}

func shortSchemaSuffix(testName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(testName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// provideDBForTest runs the REAL production startup path: AutoMigrate over the
// safe model list plus the event-sequence bootstrap/verifier.
func provideDBForTest(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	cfg := &Config{}
	cfg.Database.Driver = "mysql"
	cfg.Database.DSN = dsn
	cfg.Database.LogLevel = "silent"
	db, err := provideDB(cfg)
	if err != nil {
		t.Fatalf("provideDB: %v", err)
	}
	return db
}

func openSchema(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(MysqlDialector(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	return db
}

// mysqlColumnDefault reads a column's information_schema default.
func mysqlColumnDefault(t *testing.T, db *gorm.DB, table, column string) (string, bool) {
	t.Helper()
	var row struct {
		DefaultValue *string `gorm:"column:column_default"`
	}
	if err := db.Raw(`SELECT COLUMN_DEFAULT AS column_default
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		table, column).Scan(&row).Error; err != nil {
		t.Fatalf("read default of %s.%s: %v", table, column, err)
	}
	if row.DefaultValue == nil {
		return "", false
	}
	return *row.DefaultValue, true
}

// schemaFingerprint renders every column and index in the current schema so two
// startups can be compared for drift.
func schemaFingerprint(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var cols []struct {
		TableName  string `gorm:"column:table_name"`
		ColumnName string `gorm:"column:column_name"`
		ColumnType string `gorm:"column:column_type"`
		IsNullable string `gorm:"column:is_nullable"`
		DefaultVal string `gorm:"column:default_value"`
		Extra      string `gorm:"column:extra"`
	}
	if err := db.Raw(`SELECT TABLE_NAME AS table_name, COLUMN_NAME AS column_name, COLUMN_TYPE AS column_type,
			IS_NULLABLE AS is_nullable, COALESCE(COLUMN_DEFAULT, '') AS default_value, EXTRA AS extra
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE()`).Scan(&cols).Error; err != nil {
		t.Fatalf("read columns: %v", err)
	}
	var idx []struct {
		TableName  string `gorm:"column:table_name"`
		IndexName  string `gorm:"column:index_name"`
		NonUnique  int    `gorm:"column:non_unique"`
		SeqInIndex int    `gorm:"column:seq_in_index"`
		ColumnName string `gorm:"column:column_name"`
	}
	if err := db.Raw(`SELECT TABLE_NAME AS table_name, INDEX_NAME AS index_name, NON_UNIQUE AS non_unique,
			SEQ_IN_INDEX AS seq_in_index, COALESCE(COLUMN_NAME, '') AS column_name
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE()`).Scan(&idx).Error; err != nil {
		t.Fatalf("read indexes: %v", err)
	}
	lines := make([]string, 0, len(cols)+len(idx))
	for _, c := range cols {
		lines = append(lines, fmt.Sprintf("col|%s|%s|%s|%s|%s|%s",
			c.TableName, c.ColumnName, c.ColumnType, c.IsNullable, c.DefaultVal, c.Extra))
	}
	for _, i := range idx {
		lines = append(lines, fmt.Sprintf("idx|%s|%s|%d|%d|%s",
			i.TableName, i.IndexName, i.NonUnique, i.SeqInIndex, i.ColumnName))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// applyMySQLScript executes a `mysql` client script from Go, honouring
// DELIMITER so files that define stored procedures (which the client, not the
// server, parses) can be applied without shelling out to the CLI.
func applyMySQLScript(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for i, stmt := range splitMySQLScript(string(raw)) {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: statement %d failed: %v\n--- statement ---\n%s", path, i+1, err, stmt)
		}
	}
}

func splitMySQLScript(script string) []string {
	delimiter := ";"
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "DELIMITER ") {
			flush()
			delimiter = strings.TrimSpace(trimmed[len("DELIMITER "):])
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(strings.TrimSpace(cur.String()), delimiter) {
			stmt := strings.TrimSpace(cur.String())
			stmt = strings.TrimSpace(strings.TrimSuffix(stmt, delimiter))
			cur.Reset()
			if stmt != "" {
				out = append(out, stmt)
			}
		}
	}
	flush()
	return out
}

// --- Case A: fresh install ---------------------------------------------------

func TestMySQLMigration_FreshInstall(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := provideDBForTest(t, dsn)

	for _, col := range []string{"status", "auth_version"} {
		if !db.Migrator().HasColumn(&magi.UserModel{}, col) {
			t.Fatalf("users.%s missing after AutoMigrate", col)
		}
	}
	if got, ok := mysqlColumnDefault(t, db, "users", "status"); !ok || got != "active" {
		t.Fatalf("users.status default = %q (present=%v), want active", got, ok)
	}
	if got, ok := mysqlColumnDefault(t, db, "users", "auth_version"); !ok || got != "0" {
		t.Fatalf("users.auth_version default = %q (present=%v), want 0", got, ok)
	}

	// The production startup invariant must hold on a freshly migrated schema.
	if err := verifyEventSequenceContract(db); err != nil {
		t.Fatalf("event sequence contract on fresh schema: %v", err)
	}
	for _, model := range []any{
		&magi.EventModel{}, &magi.EventCursorModel{}, &magi.DecisionJobModel{},
		&magi.RuntimeInvocationModel{}, &magi.A2ASubmissionModel{}, &magi.UserModel{},
	} {
		if !db.Migrator().HasTable(model) {
			t.Fatalf("table missing after fresh install: %T", model)
		}
	}

	// Usable end to end: create a user, a case, and a sequenced event.
	ctx := context.Background()
	userRepo := magi.NewUserRepository(db)
	user := &entity.User{Name: "ci", Email: "ci@example.com", Role: entity.RoleUser}
	if err := userRepo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	stored, err := userRepo.GetByID(ctx, user.ID)
	if err != nil || !stored.IsActive() {
		t.Fatalf("reload user = %+v err=%v", stored, err)
	}
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(ctx, &entity.DecisionCase{ID: "ci-case", UserID: user.ID}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	event := entity.NewEvent("ci-case", "", nil, entity.EventCaseCreated, map[string]any{"v": 1})
	if err := repo.EventRepo().Create(ctx, &event); err != nil {
		t.Fatalf("create event (exercises the S16 cursor): %v", err)
	}
	if event.Seq != 1 {
		t.Fatalf("first event seq = %d, want 1", event.Seq)
	}
}

// --- Case B: existing users table upgrade ------------------------------------

// Hand-written pre-change shape: a stable contract for "what production looked
// like before status/auth_version", independent of historical model versions.
const legacyUsersDDL = `
CREATE TABLE users (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    name VARCHAR(255),
    email VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func TestMySQLMigration_UpgradesPopulatedUsersTable(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	if err := admin.Exec(legacyUsersDDL).Error; err != nil {
		t.Fatalf("seed legacy users table: %v", err)
	}
	if err := admin.Exec(`INSERT INTO users (id, name, email, role, created_at, updated_at) VALUES
		(1, 'alice', 'alice@test.com', 'admin', NOW(), NOW()),
		(2, 'bob', 'bob@test.com', 'user', NOW(), NOW())`).Error; err != nil {
		t.Fatalf("seed legacy users rows: %v", err)
	}

	db := provideDBForTest(t, dsn)

	// AutoMigrate must have added the columns without rebuilding the table or
	// losing/reordering rows.
	var rows []struct {
		ID          int64  `gorm:"column:id"`
		Name        string `gorm:"column:name"`
		Email       string `gorm:"column:email"`
		Role        string `gorm:"column:role"`
		Status      string `gorm:"column:status"`
		AuthVersion int64  `gorm:"column:auth_version"`
	}
	if err := db.Raw(`SELECT id, name, email, role, status, auth_version FROM users ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("read upgraded users: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("users after upgrade = %d rows, want 2 (no duplicate/no loss)", len(rows))
	}
	want := []struct {
		id    int64
		name  string
		email string
		role  string
	}{
		{1, "alice", "alice@test.com", "admin"},
		{2, "bob", "bob@test.com", "user"},
	}
	for i, w := range want {
		got := rows[i]
		if got.ID != w.id || got.Name != w.name || got.Email != w.email || got.Role != w.role {
			t.Fatalf("row %d = %+v, want id/name/email/role preserved as %+v", i, got, w)
		}
		if got.Status != entity.UserStatusActive {
			t.Fatalf("row %d status = %q, want active (backfilled by the new column default)", i, got.Status)
		}
		if got.AuthVersion != 0 {
			t.Fatalf("row %d auth_version = %d, want 0", i, got.AuthVersion)
		}
	}

	// The upgraded rows must be readable through the real repository and usable
	// for authentication decisions.
	user, err := magi.NewUserRepository(db).GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("load upgraded user: %v", err)
	}
	if !user.IsActive() || user.Role != entity.RoleAdmin {
		t.Fatalf("upgraded user = %+v, want an active admin", user)
	}
}

// --- Case E: AutoMigrate idempotency ----------------------------------------

func TestMySQLMigration_StartupIsIdempotent(t *testing.T) {
	dsn := newMySQLSchema(t)
	first := provideDBForTest(t, dsn)
	baseline := schemaFingerprint(t, first)

	// Production restarts run AutoMigrate every boot; the second and third must
	// neither fail (duplicate CREATE INDEX) nor drift the schema.
	provideDBForTest(t, dsn)
	third := provideDBForTest(t, dsn)
	if got := schemaFingerprint(t, third); got != baseline {
		t.Fatalf("schema drifted across repeated startups.\n--- first ---\n%s\n--- third ---\n%s", baseline, got)
	}
	if err := verifyEventSequenceContract(third); err != nil {
		t.Fatalf("event sequence contract after repeated startups: %v", err)
	}
}

// --- Cases C1/C2: the S16 operator migration and repair paths ----------------

// Pre-S16 magi_event shape as a MySQL 8 would actually have it. The s6 baseline
// file cannot be applied to MySQL 8 (TEXT columns carrying a literal DEFAULT are
// rejected with ERROR 1101), so the fixture is written explicitly: this is the
// shape a pre-S16 release produced through AutoMigrate.
const preS16EventDDL = `
CREATE TABLE magi_event (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    run_id VARCHAR(64) NOT NULL DEFAULT '',
    agent_code VARCHAR(32) NOT NULL DEFAULT '',
    type VARCHAR(64) NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    timestamp DATETIME(3) NOT NULL,
    INDEX idx_event_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// seedPreS16Events writes two events whose timestamp order is the OPPOSITE of
// their id order, so a correct backfill (ORDER BY timestamp, id) is
// distinguishable from an id-ordered one.
func seedPreS16Events(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(preS16EventDDL).Error; err != nil {
		t.Fatalf("seed pre-S16 magi_event: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_event (id, case_id, run_id, agent_code, type, payload_json, timestamp) VALUES
		('id-z', 'legacy-case', 'run-1', 'melchior', 'CASE_CREATED', '{"n":1}', '2026-01-01 00:00:01.000'),
		('id-a', 'legacy-case', 'run-1', 'melchior', 'CASE_CREATED', '{"n":2}', '2026-01-01 00:00:02.000')`).Error; err != nil {
		t.Fatalf("seed pre-S16 events: %v", err)
	}
}

// assertLegacyEventsSequenced checks the migrated rows survived and were
// sequenced in (timestamp, id) order, with a cursor ready for new writes.
func assertLegacyEventsSequenced(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := verifyEventSequenceContract(db); err != nil {
		t.Fatalf("event sequence contract: %v", err)
	}
	events, err := magi.NewRepository(db).EventRepo().ListByCase(context.Background(), "legacy-case")
	if err != nil {
		t.Fatalf("read migrated events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("migrated events = %d, want the 2 pre-existing rows", len(events))
	}
	seqByID := map[string]uint64{}
	for _, ev := range events {
		seqByID[ev.ID] = ev.Seq
	}
	if seqByID["id-z"] != 1 || seqByID["id-a"] != 2 {
		t.Fatalf("seq by id = %v, want timestamp order (id-z=1, id-a=2)", seqByID)
	}
	var cursor struct {
		NextSeq uint64 `gorm:"column:next_seq"`
	}
	if err := db.Raw(`SELECT next_seq FROM magi_event_cursor WHERE case_id = ?`, "legacy-case").Scan(&cursor).Error; err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	if cursor.NextSeq != 3 {
		t.Fatalf("cursor next_seq = %d, want 3", cursor.NextSeq)
	}
	// A post-migration write must continue the sequence.
	next := entity.NewEvent("legacy-case", "run-1", nil, entity.EventCaseCreated, map[string]any{"n": 3})
	if err := magi.NewRepository(db).EventRepo().Create(context.Background(), &next); err != nil {
		t.Fatalf("create event after migration: %v", err)
	}
	if next.Seq != 3 {
		t.Fatalf("post-migration event seq = %d, want 3", next.Seq)
	}
}

// TestMySQLMigration_S16UpgradeFromPreSequenceSchema is the documented
// operator path for an existing database: apply the S16 migration, then start
// the new binary.
func TestMySQLMigration_S16UpgradeFromPreSequenceSchema(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS16Events(t, admin)

	applyMySQLScript(t, admin, s16MigrationPath)

	db := provideDBForTest(t, dsn)
	assertLegacyEventsSequenced(t, db)
}

// TestMySQLMigration_S16ResumeFromPartialFailure covers the recovery path: a
// rollout interrupted after ADD COLUMN seq (no cursor, no backfill, no unique
// key). The published S16 cannot be re-run from here, so the resumer must be.
func TestMySQLMigration_S16ResumeFromPartialFailure(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS16Events(t, admin)
	if err := admin.Exec(`ALTER TABLE magi_event ADD COLUMN seq BIGINT UNSIGNED NULL`).Error; err != nil {
		t.Fatalf("simulate interrupted S16: %v", err)
	}
	// The published migration must indeed be unusable from this state, which is
	// why the resumer exists.
	if err := admin.Exec("ALTER TABLE magi_event ADD COLUMN seq BIGINT UNSIGNED NULL").Error; err == nil {
		t.Fatal("re-running the ADD COLUMN step should fail, confirming this is a resumed state")
	}

	applyMySQLScript(t, admin, s16ResumePath)

	db := provideDBForTest(t, dsn)
	assertLegacyEventsSequenced(t, db)
}

// --- S23: runtime identity column width --------------------------------------

// mysqlColumnWidth reads a column's declared character maximum length.
func mysqlColumnWidth(t *testing.T, db *gorm.DB, table, column string) int64 {
	t.Helper()
	var row struct {
		MaxLen *int64 `gorm:"column:max_len"`
	}
	if err := db.Raw(`SELECT CHARACTER_MAXIMUM_LENGTH AS max_len
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
		table, column).Scan(&row).Error; err != nil {
		t.Fatalf("read width of %s.%s: %v", table, column, err)
	}
	if row.MaxLen == nil {
		t.Fatalf("%s.%s not found", table, column)
	}
	return *row.MaxLen
}

func insertInvocationWithRunID(db *gorm.DB, invocationID, runID string) error {
	return db.Exec(`INSERT INTO runtime_invocation
		(invocation_id, run_id, step_id, kind, logical_ordinal, status, attempt_count, operation_name, retry_safety, input_digest, input_json, updated_at)
		VALUES (?, ?, ?, 'model', 1, 'pending', 0, 'op', 'unsafe', 'digest', '{}', NOW())`,
		invocationID, runID, "step-"+invocationID).Error
}

// narrowRuntimeIdentityColumns rewrites the three identity columns back to the
// width S21 originally published, reproducing the shape an existing database has
// before the widening.
func narrowRuntimeIdentityColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		"ALTER TABLE runtime_invocation MODIFY run_id VARCHAR(64) NOT NULL",
		"ALTER TABLE runtime_invocation_attempt MODIFY attempt_id VARCHAR(64) NOT NULL",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("narrow (%s): %v", stmt, err)
		}
	}
}

// assertRuntimeIdentityWidths pins the identity column widths. step_id stays 64
// on purpose: it is a fixed-length sha256 digest, not a generated path.
func assertRuntimeIdentityWidths(t *testing.T, db *gorm.DB, runID, stepID, attemptID int64) {
	t.Helper()
	for _, col := range []struct {
		table, column string
		want          int64
	}{
		{"runtime_invocation", "run_id", runID},
		{"runtime_invocation", "step_id", stepID},
		{"runtime_invocation_attempt", "attempt_id", attemptID},
	} {
		if got := mysqlColumnWidth(t, db, col.table, col.column); got != col.want {
			t.Fatalf("%s.%s width = %d, want %d", col.table, col.column, got, col.want)
		}
	}
}

// TestMySQLMigration_S23WidensRuntimeIdentityColumns covers the production bug
// the E2E stack exposed: run identifiers are built as
// "case-<uuid>-<role>-a<attempt>-r<round>-<phase>" and reach 69 characters, while
// the runtime identity columns were varchar(64), so every run on MySQL died with
// error 1406. It proves both upgrade paths an existing database can take — the
// startup AutoMigrate and the explicit forward-only migration — and pins the
// boundary at the declared limit so a future silent narrowing is caught.
func TestMySQLMigration_S23WidensRuntimeIdentityColumns(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS23RuntimeTables(t, admin)
	assertRuntimeIdentityWidths(t, admin, 64, 64, 64)

	// Path 1: a database that already has the narrow columns is widened by the
	// normal startup path.
	started := provideDBForTest(t, dsn)
	assertRuntimeIdentityWidths(t, started, validation.MaxInvocationRunIDBytes, 64, validation.MaxInvocationRunIDBytes)

	// Path 2: the explicit forward-only migration does the same for an operator
	// who applies Atlas files instead of relying on startup.
	narrowRuntimeIdentityColumns(t, admin)
	assertRuntimeIdentityWidths(t, admin, 64, 64, 64)
	applyMySQLScript(t, admin, s23MigrationPath)
	assertRuntimeIdentityWidths(t, admin, validation.MaxInvocationRunIDBytes, 64, validation.MaxInvocationRunIDBytes)

	// A real production identity: 41-char case id + balthasar + attempt + round
	// + phase = 69 characters, which is what the dispatcher builds.
	caseID := "case-" + strings.Repeat("c", 36)
	productionRunID := caseID + "-balthasar-a2-r1-investigate"
	if len(productionRunID) != 69 {
		t.Fatalf("fixture run id is %d chars, want the 69-char production shape", len(productionRunID))
	}
	if err := insertInvocationWithRunID(admin, "inv-production", productionRunID); err != nil {
		t.Fatalf("insert with %d-char run id: %v", len(productionRunID), err)
	}

	// Boundary: exactly the declared limit fits, one more does not.
	atLimit := strings.Repeat("x", validation.MaxInvocationRunIDBytes)
	if err := insertInvocationWithRunID(admin, "inv-at-limit", atLimit); err != nil {
		t.Fatalf("insert with %d-char run id (the declared limit): %v", len(atLimit), err)
	}
	overLimit := strings.Repeat("x", validation.MaxInvocationRunIDBytes+1)
	if err := insertInvocationWithRunID(admin, "inv-over-limit", overLimit); err == nil {
		t.Fatalf("insert with %d-char run id must be rejected by the schema", len(overLimit))
	}
}
