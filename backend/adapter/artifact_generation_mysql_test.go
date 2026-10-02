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
	"gorm.io/gorm"
)

func openArtifactGenerationMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(
		&magi.CaseModel{}, &magi.DecisionJobModel{}, &magi.DecisionJobClaimModel{},
		&magi.AgentRunModel{}, &magi.EvidenceModel{}, &magi.ClaimModel{}, &magi.VoteModel{},
		&magi.DebateRoundModel{}, &magi.ReflectionModel{}, &magi.ToolCallModel{},
		&magi.CheckpointModel{}, &magi.ResolutionModel{}, &magi.EventModel{}, &magi.EventCursorModel{},
	); err != nil {
		t.Fatalf("migrate T3 models: %v", err)
	}
	return db
}

func seedArtifactGenerationJob(t *testing.T, db *gorm.DB) (port.Repository, port.DecisionJobRepository, *entity.DecisionJob) {
	t.Helper()
	ctx := context.Background()
	caseID := "case-t3-" + uuid.NewString()
	jobID := "job-t3-" + uuid.NewString()
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(ctx, &entity.DecisionCase{
		ID: caseID, Status: entity.CaseStatusDraft,
	}); err != nil {
		t.Fatalf("create T3 case: %v", err)
	}
	if err := db.Create(&magi.DecisionJobModel{
		ID: jobID, CaseID: caseID, Status: string(entity.DecisionJobQueued),
		MaxAttempts: 4, AvailableAt: time.Now().Add(-time.Second),
	}).Error; err != nil {
		t.Fatalf("create T3 job: %v", err)
	}
	t.Cleanup(func() {
		// Order only matters for rows with application-level references; there
		// are no SQL foreign keys in this persistence layer.
		for _, table := range []string{
			"magi_tool_call", "reflection", "debate_round", "magi_vote", "claim",
			"evidence_record", "magi_agent_run", "magi_agent_checkpoint",
			"resolution", "magi_event", "magi_event_cursor", "decision_job_claim",
			"decision_job", "decision_case",
		} {
			_ = db.Exec("DELETE FROM " + table + " WHERE " + cleanupPredicate(table), caseID).Error
		}
	})
	jobs := magi.NewDecisionJobRepository(db)
	job, err := jobs.GetByCase(ctx, caseID)
	if err != nil {
		t.Fatalf("read T3 job: %v", err)
	}
	return repo, jobs, job
}

func cleanupPredicate(table string) string {
	switch table {
	case "decision_job":
		return "case_id = ?"
	case "decision_case":
		return "id = ?"
	case "decision_job_claim":
		return "case_id = ?"
	case "magi_event_cursor":
		return "case_id = ?"
	default:
		return "case_id = ?"
	}
}

func claimArtifactOwner(t *testing.T, jobs port.DecisionJobRepository, job *entity.DecisionJob, worker string) (*entity.DecisionJob, *entity.ExecutionContext) {
	t.Helper()
	claimed, ok, err := jobs.Claim(context.Background(), job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim T3 owner: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	return claimed, &entity.ExecutionContext{
		CaseID: claimed.CaseID, JobID: claimed.ID, WorkerID: worker,
		JobAttempt: claimed.Attempt, ExecutionGeneration: claimed.ExecutionGeneration,
	}
}

func requeueArtifactOwner(t *testing.T, jobs port.DecisionJobRepository, claimed *entity.DecisionJob, worker string) {
	t.Helper()
	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(context.Background(), claimed.ID, claimed.CaseID, worker, claimed.ExecutionGeneration, "t3 retry", &retryAt); err != nil {
		t.Fatalf("requeue T3 owner: %v", err)
	}
}

func TestArtifactGeneration_StaleWriterFencedAndReadsScopedOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	ctx := context.Background()
	worker := "worker-t3-" + uuid.NewString()
	first, owner1 := claimArtifactOwner(t, jobs, job, worker)
	now := time.Now()

	active := []struct {
		name string
		run  func() error
	}{
		{"agent_run", func() error { return owned.CreateAgentRunOwned(ctx, owner1, &entity.AgentRun{ID: "run-" + uuid.NewString(), CaseID: job.CaseID, StartedAt: now}) }},
		{"evidence", func() error { return owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{ID: "ev-g1-" + uuid.NewString(), CaseID: job.CaseID, CreatedAt: now}) }},
		{"claim", func() error { return owned.CreateClaimOwned(ctx, owner1, &entity.Claim{ID: "cl-" + uuid.NewString(), CaseID: job.CaseID, CreatedAt: now}) }},
		{"vote", func() error { return owned.CreateVoteOwned(ctx, owner1, &entity.Vote{ID: "vote-" + uuid.NewString(), CaseID: job.CaseID, Decision: entity.VoteDecisionApprove, CreatedAt: now}) }},
		{"debate", func() error { return owned.CreateDebateRoundOwned(ctx, owner1, &entity.DebateRound{ID: "deb-" + uuid.NewString(), CaseID: job.CaseID, StartedAt: now}) }},
		{"reflection", func() error { return owned.CreateReflectionOwned(ctx, owner1, &entity.Reflection{ID: "refl-" + uuid.NewString(), CaseID: job.CaseID, CreatedAt: now}) }},
		{"tool_call", func() error { return owned.CreateToolCallOwned(ctx, owner1, &entity.ToolCall{ID: "tool-" + uuid.NewString(), CaseID: job.CaseID, CreatedAt: now}) }},
	}
	for _, tc := range active {
		t.Run("active_" + tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatalf("active write: %v", err)
			}
		})
	}

	requeueArtifactOwner(t, jobs, first, worker)
	second, owner2 := claimArtifactOwner(t, jobs, job, worker)
	if second.ExecutionGeneration != first.ExecutionGeneration+1 {
		t.Fatalf("generation = %d after %d, want +1", second.ExecutionGeneration, first.ExecutionGeneration)
	}

	staleID := "ev-stale-" + uuid.NewString()
	if err := owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{
		ID: staleID, CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, CreatedAt: now,
	}); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("stale writer error = %v, want ErrLeaseLost", err)
	}
	var staleRows int64
	if err := db.Model(&magi.EvidenceModel{}).Where("id = ?", staleID).Count(&staleRows).Error; err != nil {
		t.Fatalf("count stale evidence: %v", err)
	}
	if staleRows != 0 {
		t.Fatalf("stale writer inserted %d row(s)", staleRows)
	}

	gen2ID := "ev-g2-" + uuid.NewString()
	if err := owned.CreateEvidenceOwned(ctx, owner2, &entity.EvidenceRecord{
		ID: gen2ID, CaseID: job.CaseID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create gen2 evidence: %v", err)
	}
	gen1, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, first.ExecutionGeneration)
	if err != nil || len(gen1) != 1 {
		t.Fatalf("gen1 evidence len=%d err=%v, want 1", len(gen1), err)
	}
	gen2, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, second.ExecutionGeneration)
	if err != nil || len(gen2) != 1 || gen2[0].ID != gen2ID {
		t.Fatalf("gen2 evidence=%+v err=%v", gen2, err)
	}
	history, err := repo.EvidenceRepo().ListByCase(ctx, job.CaseID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history evidence len=%d err=%v, want 2 generations", len(history), err)
	}

	unfencedID := "ev-unfenced-" + uuid.NewString()
	if err := repo.EvidenceRepo().Create(ctx, &entity.EvidenceRecord{
		ID: unfencedID, CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, CreatedAt: now,
	}); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("legacy positive-generation Create error = %v, want ErrLeaseLost", err)
	}
}

func TestArtifactGeneration_CheckpointDoesNotCrossGenerationOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	checkpoints := repo.(port.GenerationCheckpointRepository)
	ctx := context.Background()
	worker := "worker-cp-" + uuid.NewString()
	first, owner1 := claimArtifactOwner(t, jobs, job, worker)
	runID := "logical-run-" + uuid.NewString()

	if err := checkpoints.SaveForExecution(ctx, owner1, &entity.AgentState{
		CaseID: job.CaseID, RunID: runID, ExecutionGeneration: first.ExecutionGeneration, StepCount: 3,
	}); err != nil {
		t.Fatalf("save gen1 checkpoint: %v", err)
	}
	requeueArtifactOwner(t, jobs, first, worker)
	second, owner2 := claimArtifactOwner(t, jobs, job, worker)

	if got, err := checkpoints.LoadForExecution(ctx, owner2, runID); err != nil || got != nil {
		t.Fatalf("gen2 restored gen1 checkpoint: state=%+v err=%v", got, err)
	}
	if err := checkpoints.SaveForExecution(ctx, owner1, &entity.AgentState{
		CaseID: job.CaseID, RunID: runID, ExecutionGeneration: first.ExecutionGeneration, StepCount: 4,
	}); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("stale checkpoint save error = %v, want ErrLeaseLost", err)
	}
	if err := checkpoints.SaveForExecution(ctx, owner2, &entity.AgentState{
		CaseID: job.CaseID, RunID: runID, ExecutionGeneration: second.ExecutionGeneration, StepCount: 1,
	}); err != nil {
		t.Fatalf("save gen2 checkpoint: %v", err)
	}
	got, err := checkpoints.LoadForExecution(ctx, owner2, runID)
	if err != nil || got == nil || got.ExecutionGeneration != second.ExecutionGeneration || got.StepCount != 1 {
		t.Fatalf("gen2 checkpoint=%+v err=%v", got, err)
	}
	if legacy, err := repo.CheckpointRepo().Load(ctx, runID); err != nil || legacy != nil {
		t.Fatalf("legacy load exposed positive generation: state=%+v err=%v", legacy, err)
	}
	var rows int64
	if err := db.Model(&magi.CheckpointModel{}).Where("run_id = ?", runID).Count(&rows).Error; err != nil {
		t.Fatalf("count checkpoints: %v", err)
	}
	if rows != 2 {
		t.Fatalf("checkpoint rows = %d, want 2 generations", rows)
	}
}

func TestArtifactGeneration_TerminalReferencesRequireSameGenerationOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	committer := repo.(port.TerminalCommitter)
	ctx := context.Background()
	worker := "worker-term-" + uuid.NewString()
	first, owner1 := claimArtifactOwner(t, jobs, job, worker)
	now := time.Now()

	voteOld := "vote-old-" + uuid.NewString()
	evOld := "ev-old-" + uuid.NewString()
	clOld := "cl-old-" + uuid.NewString()
	if err := owned.CreateVoteOwned(ctx, owner1, &entity.Vote{ID: voteOld, CaseID: job.CaseID, Decision: entity.VoteDecisionApprove, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{ID: evOld, CaseID: job.CaseID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := owned.CreateClaimOwned(ctx, owner1, &entity.Claim{ID: clOld, CaseID: job.CaseID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	requeueArtifactOwner(t, jobs, first, worker)
	second, owner2 := claimArtifactOwner(t, jobs, job, worker)

	for name, res := range map[string]*entity.Resolution{
		"vote":     {ID: "res-v-" + uuid.NewString(), CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, VoteIDs: []string{voteOld}},
		"evidence": {ID: "res-e-" + uuid.NewString(), CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, KeyEvidenceIDs: []string{evOld}},
		"claim":    {ID: "res-c-" + uuid.NewString(), CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, KeyClaimIDs: []string{clOld}},
	} {
		t.Run(name, func(t *testing.T) {
			event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseCompleted, map[string]any{"kind": name})
			committed, err := committer.CommitTerminal(ctx, job.CaseID, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
			if err == nil || committed {
				t.Fatalf("cross-generation reference committed=%v err=%v", committed, err)
			}
		})
	}

	voteNew := "vote-new-" + uuid.NewString()
	evNew := "ev-new-" + uuid.NewString()
	clNew := "cl-new-" + uuid.NewString()
	if err := owned.CreateVoteOwned(ctx, owner2, &entity.Vote{ID: voteNew, CaseID: job.CaseID, Decision: entity.VoteDecisionApprove, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := owned.CreateEvidenceOwned(ctx, owner2, &entity.EvidenceRecord{ID: evNew, CaseID: job.CaseID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := owned.CreateClaimOwned(ctx, owner2, &entity.Claim{ID: clNew, CaseID: job.CaseID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	res := &entity.Resolution{
		ID: "res-ok-" + uuid.NewString(), CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration,
		VoteIDs: []string{voteNew}, KeyEvidenceIDs: []string{evNew}, KeyClaimIDs: []string{clNew},
		FinalDecision: entity.VoteDecisionApprove, CreatedAt: now,
	}
	event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseCompleted, nil)
	committed, err := committer.CommitTerminal(ctx, job.CaseID, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || !committed {
		t.Fatalf("same-generation terminal commit=%v err=%v", committed, err)
	}
	stored, err := repo.ResolutionRepo().Get(ctx, job.CaseID)
	if err != nil || stored.ExecutionGeneration != second.ExecutionGeneration {
		t.Fatalf("stored resolution=%+v err=%v", stored, err)
	}
}

func TestArtifactGeneration_OwnerCheckAndInsertShareLinearizationOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	owned := repo.(port.OwnedArtifactRepository)
	ctx := context.Background()
	worker := "worker-linear-" + uuid.NewString()
	first, owner := claimArtifactOwner(t, jobs, job, worker)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql DB: %v", err)
	}
	blocker, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatalf("blocker conn: %v", err)
	}
	defer blocker.Close()

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	gateName := "t3_gate_" + suffix[:24]
	enteredName := "t3_entered_" + suffix[:21]
	triggerName := "t3_block_ev_" + suffix[:18]
	var got int
	if err := blocker.QueryRowContext(ctx, "SELECT GET_LOCK(?, 2)", gateName).Scan(&got); err != nil || got != 1 {
		t.Fatalf("acquire gate: got=%d err=%v", got, err)
	}
	defer func() {
		var released sql.NullInt64
		_ = blocker.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", gateName).Scan(&released)
		_ = db.Exec("DROP TRIGGER IF EXISTS " + triggerName).Error
	}()

	trigger := fmt.Sprintf(`CREATE TRIGGER %s
		BEFORE INSERT ON evidence_record
		FOR EACH ROW
		BEGIN
			SET @t3_entered = GET_LOCK('%s', 0);
			SET @t3_gate = GET_LOCK('%s', 10);
			SET @t3_release_gate = RELEASE_LOCK('%s');
			SET @t3_release_entered = RELEASE_LOCK('%s');
		END`, triggerName, enteredName, gateName, gateName, enteredName)
	if err := db.Exec(trigger).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	evidenceID := "ev-linear-" + uuid.NewString()
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- owned.CreateEvidenceOwned(ctx, owner, &entity.EvidenceRecord{
			ID: evidenceID, CaseID: job.CaseID, CreatedAt: time.Now(),
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	entered := false
	for time.Now().Before(deadline) {
		var holder sql.NullInt64
		if err := db.Raw("SELECT IS_USED_LOCK(?)", enteredName).Scan(&holder).Error; err != nil {
			t.Fatalf("observe trigger: %v", err)
		}
		if holder.Valid {
			entered = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !entered {
		t.Fatal("artifact INSERT did not reach trigger barrier")
	}

	transitionDone := make(chan error, 1)
	retryAt := time.Now().Add(-time.Second)
	go func() {
		transitionDone <- jobs.MarkFailed(ctx, first.ID, first.CaseID, worker, first.ExecutionGeneration, "linearization", &retryAt)
	}()
	select {
	case err := <-transitionDone:
		t.Fatalf("owner transition completed while INSERT was blocked: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	var released sql.NullInt64
	if err := blocker.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", gateName).Scan(&released); err != nil {
		t.Fatalf("release gate: %v", err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("artifact write: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("artifact writer stayed blocked")
	}
	select {
	case err := <-transitionDone:
		if err != nil {
			t.Fatalf("owner transition after write: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner transition stayed blocked")
	}
	var rows int64
	if err := db.Model(&magi.EvidenceModel{}).Where("id = ?", evidenceID).Count(&rows).Error; err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	if rows != 1 {
		t.Fatalf("evidence rows=%d, want 1", rows)
	}
}
