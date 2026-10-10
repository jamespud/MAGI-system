package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	mysqlcfg "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

func t6Account(t *testing.T, db *gorm.DB, prefix string) (string, string) {
	t.Helper()
	user := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")[:14]
	password := uuid.NewString()
	if err := db.Exec("CREATE USER '" + user + "'@'%' IDENTIFIED BY '" + password + "'").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP USER IF EXISTS '" + user + "'@'%'").Error })
	return user + "@%", password
}
func t6DSN(t *testing.T, dsn, account, password string) string {
	t.Helper()
	cfg, err := mysqlcfg.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.User = strings.Split(account, "@")[0]
	cfg.Passwd = password
	return cfg.FormatDSN()
}
func seedT6Result(t *testing.T, db *gorm.DB) string {
	t.Helper()
	ctx := context.Background()
	repo := magi.NewRepository(db)
	c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	if err := repo.CaseRepo().Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	job, _, err := jobs.Admit(ctx, c.ID, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, ok, err := jobs.Claim(ctx, job.ID, "w", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	retry := time.Now().Add(-time.Second)
	if err = jobs.MarkFailed(ctx, job.ID, c.ID, "w", first.ExecutionGeneration, "retry", &retry); err != nil {
		t.Fatal(err)
	}
	second, ok, err := jobs.Claim(ctx, job.ID, "w", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatal(err)
	}
	owner := &entity.ExecutionContext{CaseID: c.ID, JobID: job.ID, WorkerID: "w", ExecutionGeneration: second.ExecutionGeneration}
	run, evidence, claim, vote := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	owned := repo.(port.OwnedArtifactRepository)
	writes := []func() error{
		func() error {
			return owned.CreateAgentRunOwned(ctx, owner, &entity.AgentRun{ID: run, CaseID: c.ID, StartedAt: time.Now()})
		},
		func() error {
			return owned.CreateEvidenceOwned(ctx, owner, &entity.EvidenceRecord{ID: evidence, CaseID: c.ID, AgentRunID: run})
		},
		func() error {
			return owned.CreateClaimOwned(ctx, owner, &entity.Claim{ID: claim, CaseID: c.ID, AgentRunID: run, Supports: []string{evidence}})
		},
		func() error {
			return owned.CreateVoteOwned(ctx, owner, &entity.Vote{ID: vote, CaseID: c.ID, AgentRunID: run, EvidenceIDs: []string{evidence}, KeyClaimIDs: []string{claim}})
		},
		func() error {
			return owned.CreateReflectionOwned(ctx, owner, &entity.Reflection{ID: uuid.NewString(), CaseID: c.ID, AgentRunID: run})
		},
		func() error {
			return owned.CreateToolCallOwned(ctx, owner, &entity.ToolCall{ID: uuid.NewString(), CaseID: c.ID, AgentRunID: run, EvidenceID: evidence})
		},
		func() error {
			return owned.CreateDebateRoundOwned(ctx, owner, &entity.DebateRound{ID: uuid.NewString(), CaseID: c.ID, StartedAt: time.Now()})
		},
		func() error {
			return repo.(port.GenerationCheckpointRepository).SaveForExecution(ctx, owner, &entity.AgentState{RunID: "run-" + c.ID, CaseID: c.ID})
		},
	}
	for _, write := range writes {
		if err := write(); err != nil {
			t.Fatal(err)
		}
	}
	res := &entity.Resolution{ID: uuid.NewString(), CaseID: c.ID, ExecutionGeneration: 2, VoteIDs: []string{vote}, KeyEvidenceIDs: []string{evidence}, KeyClaimIDs: []string{claim}}
	event := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
	event.ExecutionGeneration = 2
	if ok, err := repo.(port.OwnedCaseCommitter).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event); err != nil || !ok {
		t.Fatal(err)
	}
	return c.ID
}

func TestMySQLDecisionWriter_S29MigrationAndFailClosedUpgrade(t *testing.T) {
	_, db := t6SchemaFixture(t)
	if err := db.Migrator().DropTable(&magi.DecisionWriterContractModel{}); err != nil {
		t.Fatal(err)
	}
	applyMySQLScript(t, db, "../../docker/atlas/migrations/magi_s29_decision_writer_contract.sql")
	script := mysqlTableFingerprint(t, db, "decision_writer_contract")
	if err := db.Migrator().DropTable(&magi.DecisionWriterContractModel{}); err != nil {
		t.Fatal(err)
	}
	if err := InstallDecisionWriterContract(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if got := mysqlTableFingerprint(t, db, "decision_writer_contract"); got != script {
		t.Fatalf("S29 parity mismatch\n%s\n%s", script, got)
	}
	var gate magi.DecisionWriterContractModel
	db.First(&gate)
	if gate.AdmissionState != magi.DecisionAdmissionBlocked || gate.CutoverEpoch != 0 || !gate.LegacyRollbackForbidden {
		t.Fatal("unsafe S29 defaults")
	}
	if err := InstallDecisionWriterContract(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE magi_vote DROP COLUMN execution_generation").Error; err != nil {
		t.Fatal(err)
	}
	before := schemaFingerprint(t, db)
	if err := InstallDecisionWriterContract(context.Background(), db); err == nil {
		t.Fatal("upgrade silently repaired partial S27")
	}
	if schemaFingerprint(t, db) != before {
		t.Fatal("failed upgrade changed schema")
	}
	if err := EnableDecisionWriter(context.Background(), db, 0); err == nil {
		t.Fatal("enabled after failed migration")
	}
	db.First(&gate)
	if gate.AdmissionState != magi.DecisionAdmissionBlocked {
		t.Fatal("failure reopened gate")
	}
}

func TestMySQLDecisionWriter_CutoverExcludesActualOldBinary(t *testing.T) {
	dsn, db := t6SchemaFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	binary := os.Getenv("MAGI_T6_LEGACY_PROBE")
	if binary == "" {
		target := t.TempDir()
		build := exec.CommandContext(ctx, "bash", "../../scripts/build-t6-legacy-probe.sh", target)
		build.Env = os.Environ()
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("pinned pre-T1 binary build: %v\n%s", err, output)
		}
		binary = filepath.Join(target, "legacy-probe")
	}
	legacy, password := t6Account(t, db, "t6_old_")
	var schema string
	db.Raw("SELECT DATABASE()").Scan(&schema)
	if err := db.Exec("GRANT SELECT,INSERT,UPDATE,DELETE ON `" + schema + "`.* TO '" + strings.Split(legacy, "@")[0] + "'@'%'").Error; err != nil {
		t.Fatal(err)
	}
	legacyDSN := t6DSN(t, dsn, legacy, password)
	runProbe := func(caseID, mode string, exclude func()) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary)
		cmd.Env = append(os.Environ(), "T6_PROBE_DSN="+legacyDSN, "T6_PROBE_CASE="+caseID, "T6_PROBE_MODE="+mode)
		stdin, _ := cmd.StdinPipe()
		stdout, _ := cmd.StdoutPipe()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		scanner := bufio.NewScanner(stdout)
		if !scanner.Scan() || !strings.HasPrefix(scanner.Text(), "READY ") {
			t.Fatal("old authenticated connection was not established")
		}
		if exclude != nil {
			exclude()
		}
		_, _ = fmt.Fprintln(stdin, "release")
		_ = stdin.Close()
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("old binary probe: %v %s", err, stderr.String())
		}
		return strings.Join(lines, " ")
	}
	vulnerable := seedT6Result(t, db)
	if result := runProbe(vulnerable, "control", nil); result != "STATUS_WRITTEN CLEANUP_WRITTEN" {
		t.Fatal(result)
	}
	var n int64
	db.Model(&magi.VoteModel{}).Where("case_id=?", vulnerable).Count(&n)
	var damaged magi.CaseModel
	db.Where("id=?", vulnerable).First(&damaged)
	if n != 0 || damaged.Status != "FAILED" || damaged.ExecutionGeneration != 2 {
		t.Fatal("old binary positive control did not expose original bypass")
	}
	protected := seedT6Result(t, db)
	// New account is created by the management protocol, not granted schema-wide authority.
	writer := "t6_new_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:14] + "@%"
	writerPassword := uuid.NewString()
	t.Cleanup(func() { _ = db.Exec("DROP USER IF EXISTS '" + strings.Split(writer, "@")[0] + "'@'%'").Error })
	if err := ProvisionDecisionWriter(ctx, db, writer, writerPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := BlockDecisionWriter(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureDecisionWriter(ctx, db, writer, legacy); err != nil {
		t.Fatal(err)
	}
	before := t6Snapshot(t, db)
	if result := runProbe(protected, "denied", func() {
		if err := ExcludeLegacyWriter(ctx, db); err != nil {
			t.Fatal(err)
		}
	}); result != "STATUS_DENIED CLEANUP_DENIED" {
		t.Fatal(result)
	}
	if !reflect.DeepEqual(before, t6Snapshot(t, db)) {
		t.Fatal("old connection changed authoritative write set")
	}
	reconnect := exec.CommandContext(ctx, binary)
	reconnect.Env = append(os.Environ(), "T6_PROBE_DSN="+legacyDSN, "T6_PROBE_MODE=denied")
	if result, err := reconnect.CombinedOutput(); err != nil || strings.TrimSpace(string(result)) != "CONNECT_DENIED" {
		t.Fatal("old credentials can reconnect")
	}
	// Stale enable cannot reopen a later cutover.
	var gate magi.DecisionWriterContractModel
	db.First(&gate)
	if err := EnableDecisionWriter(ctx, db, gate.CutoverEpoch-1); err == nil {
		t.Fatal("stale operator enabled")
	}
	// Premature enable with a live old Job is rejected even after credentials
	// are excluded. Operators must explicitly settle/cancel it.
	pending := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	if err := magi.NewRepository(db).CaseRepo().Create(ctx, pending); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.DecisionJobModel{ID: uuid.NewString(), CaseID: pending.ID, Status: "RUNNING", MaxAttempts: 1, AvailableAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := EnableDecisionWriter(ctx, db, gate.CutoverEpoch); err == nil {
		t.Fatal("premature enable ignored RUNNING writer")
	}
	if _, err := magi.NewDecisionWriterRepository(db).(port.CaseControlCommitter).CommitCaseControl(ctx, pending.ID, entity.CaseStatusCancelled); err != nil {
		t.Fatal(err)
	}
	if err := EnableDecisionWriter(ctx, db, gate.CutoverEpoch); err != nil {
		t.Fatal(err)
	}
	newDSN := t6DSN(t, dsn, writer, writerPassword)
	cfg := &Config{}
	cfg.Database.DSN = newDSN
	cfg.Database.LogLevel = "silent"
	runtimeDB, err := provideDB(cfg)
	if err != nil {
		t.Fatalf("new writer startup: %v", err)
	}
	defer func() { p, _ := runtimeDB.DB(); _ = p.Close() }()
	if err := runtimeDB.Exec("UPDATE decision_writer_contract SET admission_state='BLOCKED' WHERE id=1").Error; err == nil {
		t.Fatal("runtime can change its own gate")
	}
	if err := runtimeDB.Exec("ALTER TABLE decision_case ADD COLUMN illicit INT").Error; err == nil {
		t.Fatal("runtime retains DDL")
	}
	liveRepo := magi.NewDecisionWriterRepository(runtimeDB)
	liveCase := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	if err := liveRepo.CaseRepo().Create(ctx, liveCase); err != nil {
		t.Fatal(err)
	}
	liveJobs := magi.NewDecisionJobRepository(runtimeDB)
	admitted, ok, err := liveJobs.Admit(ctx, liveCase.ID, 3, 0)
	if err != nil || !ok {
		t.Fatalf("new identity admission: %v", err)
	}
	claimed, ok, err := liveJobs.Claim(ctx, admitted.ID, "new-writer", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed.ExecutionGeneration != 1 {
		t.Fatalf("new identity claim: %v", err)
	}
	fresh, err := liveRepo.CaseRepo().Get(ctx, liveCase.ID)
	if err != nil {
		t.Fatal(err)
	}
	executor := &t6OwnedOrchestrator{repo: liveRepo, called: make(chan *entity.ExecutionContext, 1)}
	owner := &entity.ExecutionContext{CaseID: liveCase.ID, JobID: claimed.ID, WorkerID: "new-writer", ExecutionGeneration: 1}
	if _, err := executor.OrchestrateForExecution(ctx, fresh, owner); err != nil {
		t.Fatalf("new identity settlement: %v", err)
	}
	if _, err := BlockDecisionWriter(ctx, db); err != nil {
		t.Fatal(err)
	}
	// Irreversible history survives deleting all positive generation rows.
	if err := db.Exec("DELETE FROM decision_case WHERE execution_generation>0").Error; err != nil {
		t.Fatal(err)
	}
	db.First(&gate)
	if !gate.LegacyRollbackForbidden || gate.AdmissionState != magi.DecisionAdmissionBlocked {
		t.Fatal("rollback marker erased")
	}
}
