package magi_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// singleConnPool pins one test's repository to a single MySQL connection so a
// session variable (innodb_lock_wait_timeout) applies to every statement the
// repository issues. Without it the pool may hand a different session to the
// verification query and the injected fault becomes non-deterministic.
func singleConnPool(t *testing.T, db interface{ DB() (*sql.DB, error) }) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
}

// P1: an error raised while verifying the durable owner (here: the
// SELECT ... FOR UPDATE against decision_job times out on a competing lock) is
// a storage failure, not a lost lease. It must surface as-is so the caller can
// abandon the execution instead of the old log-and-continue path.
//
// The competing lock is held on a separate pool because the repository pool is
// pinned to one connection for the session-scoped lock timeout.
func TestArtifactGeneration_OwnerVerificationQueryErrorIsNotLeaseLost(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	singleConnPool(t, db)
	if err := db.Exec("SET SESSION innodb_lock_wait_timeout = 1").Error; err != nil {
		t.Fatalf("set lock wait timeout: %v", err)
	}

	ctx := context.Background()
	worker := "worker-owner-err-" + uuid.NewString()
	_, owner := claimArtifactOwner(t, jobs, job, worker)

	// Competing lock on the exact row the owner predicate reads first.
	blocker := openA2AMySQL(t)
	blockerTx := blocker.Begin()
	if blockerTx.Error != nil {
		t.Fatalf("begin blocker tx: %v", blockerTx.Error)
	}
	defer func() { _ = blockerTx.Rollback().Error }()
	var lockedID string
	if err := blockerTx.Raw("SELECT id FROM decision_job WHERE id = ? FOR UPDATE", job.ID).Scan(&lockedID).Error; err != nil {
		t.Fatalf("hold decision_job lock: %v", err)
	}

	ev := &entity.EvidenceRecord{ID: "ev-owner-err-" + uuid.NewString(), CaseID: job.CaseID, CreatedAt: time.Now()}
	start := time.Now()
	err := owned.CreateEvidenceOwned(ctx, owner, ev)
	if err == nil {
		t.Fatal("owner verification lock timeout was reported as a successful write")
	}
	if errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("owner verification query error was collapsed into ErrLeaseLost: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lock wait timeout") {
		t.Fatalf("error = %v, want the real MySQL lock wait timeout", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("verification returned after %s; the query did not actually block on the lock", elapsed)
	}
	var rows int64
	if err := db.Model(&magi.EvidenceModel{}).Where("id = ?", ev.ID).Count(&rows).Error; err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if rows != 0 {
		t.Fatalf("evidence rows after a failed owner verification = %d, want 0", rows)
	}
	// The owner is still the active worker: the failure is transient storage
	// trouble, not ownership loss, and the same generation may retry.
	if err := blockerTx.Rollback().Error; err != nil {
		t.Fatalf("release blocker: %v", err)
	}
	if err := owned.CreateEvidenceOwned(ctx, owner, ev); err != nil {
		t.Fatalf("retry with a healthy lock: %v", err)
	}
}

// P1: an INSERT that fails inside the authoritative transaction must be
// reported as an error (never tolerated), and the transaction must leave no
// artifact behind. The trigger injects a real MySQL statement failure that is
// not a lease-loss signal.
func TestArtifactGeneration_ArtifactInsertErrorIsNotLeaseLost(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	ctx := context.Background()
	worker := "worker-insert-err-" + uuid.NewString()
	_, owner := claimArtifactOwner(t, jobs, job, worker)

	runID := "run-insert-err-" + uuid.NewString()
	trigger := "t3_insert_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	ddl := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON magi_agent_run
		FOR EACH ROW
		BEGIN
			IF NEW.id = '%s' THEN
				SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 't3 injected agent-run insert failure';
			END IF;
		END`, trigger, runID)
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create insert-failure trigger: %v", err)
	}
	defer func() { _ = db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error }()

	err := owned.CreateAgentRunOwned(ctx, owner, &entity.AgentRun{ID: runID, CaseID: job.CaseID, StartedAt: time.Now()})
	if err == nil {
		t.Fatal("injected INSERT failure was reported as a successful write")
	}
	if errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("INSERT failure was collapsed into ErrLeaseLost: %v", err)
	}
	if !strings.Contains(err.Error(), "t3 injected agent-run insert failure") {
		t.Fatalf("error = %v, want the injected MySQL statement failure", err)
	}
	var rows int64
	if err := db.Model(&magi.AgentRunModel{}).Where("id = ?", runID).Count(&rows).Error; err != nil {
		t.Fatalf("count agent runs: %v", err)
	}
	if rows != 0 {
		t.Fatalf("agent run rows after a failed INSERT = %d, want 0", rows)
	}

	// The failed statement is recoverable: dropping the trigger frees the same
	// owner/generation to write again.
	if err := db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	if err := owned.CreateAgentRunOwned(ctx, owner, &entity.AgentRun{ID: runID, CaseID: job.CaseID, StartedAt: time.Now()}); err != nil {
		t.Fatalf("retry after the INSERT failure: %v", err)
	}
}

// P1, unknown-outcome boundary: when the write is abandoned while the server
// statement is still in flight, the client cannot know whether the row landed.
// The repository must therefore never report success. Recovery is not "guess
// that it worked" -- the generation is abandoned and a later generation writes
// its own generation-scoped row.
//
// What this test does NOT prove: a literal post-COMMIT reply loss (the server
// commits, then the client never receives the acknowledgement). GORM/go-sql
// expose no seam to drop only the COMMIT reply, so that boundary stays an
// explicit, untested assumption rather than a claimed guarantee.
func TestArtifactGeneration_AbandonedInFlightWriteIsNotReportedCommitted(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	worker := "worker-abandon-" + uuid.NewString()
	first, owner1 := claimArtifactOwner(t, jobs, job, worker)

	runID := "run-abandon-" + uuid.NewString()
	trigger := "t3_slow_insert_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:19]
	ddl := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON magi_agent_run
		FOR EACH ROW
		BEGIN
			IF NEW.id = '%s' THEN
				DO SLEEP(2);
			END IF;
		END`, trigger, runID)
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create slow-insert trigger: %v", err)
	}
	defer func() { _ = db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error }()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	err := owned.CreateAgentRunOwned(ctx, owner1, &entity.AgentRun{ID: runID, CaseID: job.CaseID, StartedAt: time.Now()})
	if err == nil {
		t.Fatal("an abandoned in-flight authoritative write was reported as committed")
	}

	// Whether the aborted statement eventually landed is exactly what the
	// client cannot know; the observable contract is the abandoned generation.
	// A later generation still owns the case, so requeue and recover.
	if err := db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error; err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	requeueArtifactOwner(t, jobs, first, worker)
	second, owner2 := claimArtifactOwner(t, jobs, job, worker)
	if second.ExecutionGeneration == first.ExecutionGeneration {
		t.Fatalf("recovery reused generation %d; the abandoned generation must be superseded", first.ExecutionGeneration)
	}
	if err := owned.CreateAgentRunOwned(context.Background(), owner2, &entity.AgentRun{ID: runID, CaseID: job.CaseID, StartedAt: time.Now()}); err != nil {
		t.Fatalf("recover on new generation: %v", err)
	}
	gen2, err := owned.ListAgentRunsByGeneration(context.Background(), job.CaseID, second.ExecutionGeneration)
	if err != nil || len(gen2) != 1 || gen2[0].ID != runID {
		t.Fatalf("gen%d read = %+v err=%v", second.ExecutionGeneration, gen2, err)
	}
}
