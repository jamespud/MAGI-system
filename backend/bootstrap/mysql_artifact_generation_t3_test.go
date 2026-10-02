package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func claimT3Owner(t *testing.T, jobs port.DecisionJobRepository, job *entity.DecisionJob, worker string) (*entity.DecisionJob, *entity.ExecutionContext) {
	t.Helper()
	claimed, ok, err := jobs.Claim(context.Background(), job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed == nil {
		t.Fatalf("claim T3 owner: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	owner := &entity.ExecutionContext{
		CaseID: claimed.CaseID, JobID: claimed.ID, WorkerID: worker,
		JobAttempt: claimed.Attempt, ExecutionGeneration: claimed.ExecutionGeneration,
	}
	return claimed, owner
}

func requeueT3Owner(t *testing.T, jobs port.DecisionJobRepository, claimed *entity.DecisionJob, worker string) {
	t.Helper()
	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(context.Background(), claimed.ID, claimed.CaseID, worker, claimed.ExecutionGeneration, "t3 retry", &retryAt); err != nil {
		t.Fatalf("requeue T3 owner: %v", err)
	}
}

func TestMySQLArtifactGeneration_StaleWriterFencedAndReadsStayGenerationScoped(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-t3-artifacts", 4)
	owned, ok := repo.(port.OwnedArtifactRepository)
	if !ok {
		t.Fatal("repository is missing OwnedArtifactRepository")
	}
	ctx := context.Background()
	worker := "worker-t3-artifacts"
	first, owner1 := claimT3Owner(t, jobs, job, worker)
	now := time.Now()

	activeWrites := []struct {
		name string
		id   string
		run  func() error
	}{
		{"agent_run", "run-g1", func() error {
			return owned.CreateAgentRunOwned(ctx, owner1, &entity.AgentRun{ID: "run-g1", CaseID: job.CaseID, MagiCode: "melchior", StartedAt: now})
		}},
		{"evidence", "ev-g1", func() error {
			return owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{ID: "ev-g1", CaseID: job.CaseID, AgentRunID: "run-g1", CreatedAt: now})
		}},
		{"claim", "cl-g1", func() error {
			return owned.CreateClaimOwned(ctx, owner1, &entity.Claim{ID: "cl-g1", CaseID: job.CaseID, AgentRunID: "run-g1", CreatedAt: now})
		}},
		{"vote", "vote-g1", func() error {
			return owned.CreateVoteOwned(ctx, owner1, &entity.Vote{ID: "vote-g1", CaseID: job.CaseID, AgentRunID: "run-g1", Decision: entity.VoteDecisionApprove, CreatedAt: now})
		}},
		{"debate", "deb-g1", func() error {
			return owned.CreateDebateRoundOwned(ctx, owner1, &entity.DebateRound{ID: "deb-g1", CaseID: job.CaseID, Round: 1, StartedAt: now})
		}},
		{"reflection", "refl-g1", func() error {
			return owned.CreateReflectionOwned(ctx, owner1, &entity.Reflection{ID: "refl-g1", CaseID: job.CaseID, AgentRunID: "run-g1", Round: 1, CreatedAt: now})
		}},
		{"tool_call", "tool-g1", func() error {
			return owned.CreateToolCallOwned(ctx, owner1, &entity.ToolCall{ID: "tool-g1", CaseID: job.CaseID, AgentRunID: "run-g1", CreatedAt: now})
		}},
	}
	for _, tc := range activeWrites {
		t.Run("active_"+tc.name, func(t *testing.T) {
			if err := tc.run(); err != nil {
				t.Fatalf("active generation write failed: %v", err)
			}
		})
	}

	requeueT3Owner(t, jobs, first, worker)
	second, owner2 := claimT3Owner(t, jobs, job, worker)
	if second.ExecutionGeneration != first.ExecutionGeneration+1 {
		t.Fatalf("generation did not advance: first=%d second=%d", first.ExecutionGeneration, second.ExecutionGeneration)
	}

	staleWrites := []struct {
		name  string
		id    string
		table string
		run   func() error
	}{
		{"agent_run", "run-g1-late", "magi_agent_run", func() error {
			return owned.CreateAgentRunOwned(ctx, owner1, &entity.AgentRun{ID: "run-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, StartedAt: now})
		}},
		{"evidence", "ev-g1-late", "evidence_record", func() error {
			return owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{ID: "ev-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, CreatedAt: now})
		}},
		{"claim", "cl-g1-late", "claim", func() error {
			return owned.CreateClaimOwned(ctx, owner1, &entity.Claim{ID: "cl-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, CreatedAt: now})
		}},
		{"vote", "vote-g1-late", "magi_vote", func() error {
			return owned.CreateVoteOwned(ctx, owner1, &entity.Vote{ID: "vote-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, Decision: entity.VoteDecisionApprove, CreatedAt: now})
		}},
		{"debate", "deb-g1-late", "debate_round", func() error {
			return owned.CreateDebateRoundOwned(ctx, owner1, &entity.DebateRound{ID: "deb-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, StartedAt: now})
		}},
		{"reflection", "refl-g1-late", "reflection", func() error {
			return owned.CreateReflectionOwned(ctx, owner1, &entity.Reflection{ID: "refl-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, CreatedAt: now})
		}},
		{"tool_call", "tool-g1-late", "magi_tool_call", func() error {
			return owned.CreateToolCallOwned(ctx, owner1, &entity.ToolCall{ID: "tool-g1-late", CaseID: job.CaseID, ExecutionGeneration: owner1.ExecutionGeneration, CreatedAt: now})
		}},
	}
	for _, tc := range staleWrites {
		t.Run("stale_"+tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("stale write error = %v, want ErrLeaseLost", err)
			}
			var n int64
			if err := db.Table(tc.table).Where("id = ?", tc.id).Count(&n).Error; err != nil {
				t.Fatalf("count stale row: %v", err)
			}
			if n != 0 {
				t.Fatalf("stale write inserted %d row(s)", n)
			}
		})
	}

	if err := owned.CreateEvidenceOwned(ctx, owner2, &entity.EvidenceRecord{
		ID: "ev-g2", CaseID: job.CaseID, AgentRunID: "run-g2", CreatedAt: now,
	}); err != nil {
		t.Fatalf("create gen2 evidence: %v", err)
	}
	gen1, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, first.ExecutionGeneration)
	if err != nil || len(gen1) != 1 || gen1[0].ID != "ev-g1" {
		t.Fatalf("gen1 evidence = %+v err=%v", gen1, err)
	}
	gen2, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, second.ExecutionGeneration)
	if err != nil || len(gen2) != 1 || gen2[0].ID != "ev-g2" {
		t.Fatalf("gen2 evidence = %+v err=%v", gen2, err)
	}
	history, err := repo.EvidenceRepo().ListByCase(ctx, job.CaseID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history evidence = %+v err=%v, want both generations", history, err)
	}

	if err := repo.EvidenceRepo().Create(ctx, &entity.EvidenceRecord{
		ID: "ev-unfenced-positive", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, CreatedAt: now,
	}); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("legacy positive-generation Create error = %v, want ErrLeaseLost", err)
	}
}

func TestMySQLArtifactGeneration_CheckpointDoesNotCrossGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-t3-checkpoint", 4)
	checkpoints, ok := repo.(port.GenerationCheckpointRepository)
	if !ok {
		t.Fatal("repository is missing GenerationCheckpointRepository")
	}
	ctx := context.Background()
	worker := "worker-t3-checkpoint"
	first, owner1 := claimT3Owner(t, jobs, job, worker)
	runID := "case-t3-checkpoint-melchior-r1-investigate"

	state1 := &entity.AgentState{CaseID: job.CaseID, RunID: runID, ExecutionGeneration: first.ExecutionGeneration, StepCount: 3}
	if err := checkpoints.SaveForExecution(ctx, owner1, state1); err != nil {
		t.Fatalf("save gen1 checkpoint: %v", err)
	}

	requeueT3Owner(t, jobs, first, worker)
	second, owner2 := claimT3Owner(t, jobs, job, worker)

	got, err := checkpoints.LoadForExecution(ctx, owner2, runID)
	if err != nil || got != nil {
		t.Fatalf("gen2 restored gen1 checkpoint: state=%+v err=%v", got, err)
	}
	if err := checkpoints.SaveForExecution(ctx, owner1, &entity.AgentState{
		CaseID: job.CaseID, RunID: runID, ExecutionGeneration: first.ExecutionGeneration, StepCount: 4,
	}); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("stale checkpoint save error = %v, want ErrLeaseLost", err)
	}
	state2 := &entity.AgentState{CaseID: job.CaseID, RunID: runID, ExecutionGeneration: second.ExecutionGeneration, StepCount: 1}
	if err := checkpoints.SaveForExecution(ctx, owner2, state2); err != nil {
		t.Fatalf("save gen2 checkpoint: %v", err)
	}
	got, err = checkpoints.LoadForExecution(ctx, owner2, runID)
	if err != nil || got == nil || got.ExecutionGeneration != second.ExecutionGeneration || got.StepCount != 1 {
		t.Fatalf("gen2 checkpoint = %+v err=%v", got, err)
	}
	legacy, err := repo.CheckpointRepo().Load(ctx, runID)
	if err != nil || legacy != nil {
		t.Fatalf("legacy RunID-only load exposed positive generation: state=%+v err=%v", legacy, err)
	}
	var rows int64
	if err := db.Model(&magi.CheckpointModel{}).Where("run_id = ?", runID).Count(&rows).Error; err != nil {
		t.Fatalf("count checkpoint generations: %v", err)
	}
	if rows != 2 {
		t.Fatalf("checkpoint rows for logical RunID = %d, want 2 generations", rows)
	}
}

func TestMySQLArtifactGeneration_TerminalReferencesRequireSameGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-t3-terminal-refs", 4)
	owned := repo.(port.OwnedArtifactRepository)
	committer := repo.(port.TerminalCommitter)
	ctx := context.Background()
	worker := "worker-t3-terminal"
	first, owner1 := claimT3Owner(t, jobs, job, worker)
	now := time.Now()

	if err := owned.CreateVoteOwned(ctx, owner1, &entity.Vote{
		ID: "vote-old", CaseID: job.CaseID, Decision: entity.VoteDecisionApprove, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create old vote: %v", err)
	}
	if err := owned.CreateEvidenceOwned(ctx, owner1, &entity.EvidenceRecord{
		ID: "ev-old", CaseID: job.CaseID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create old evidence: %v", err)
	}
	if err := owned.CreateClaimOwned(ctx, owner1, &entity.Claim{
		ID: "cl-old", CaseID: job.CaseID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create old claim: %v", err)
	}

	requeueT3Owner(t, jobs, first, worker)
	second, owner2 := claimT3Owner(t, jobs, job, worker)

	cases := []struct {
		name string
		res  *entity.Resolution
	}{
		{"vote", &entity.Resolution{ID: "res-cross-vote", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, VoteIDs: []string{"vote-old"}}},
		{"evidence", &entity.Resolution{ID: "res-cross-evidence", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, KeyEvidenceIDs: []string{"ev-old"}}},
		{"claim", &entity.Resolution{ID: "res-cross-claim", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration, KeyClaimIDs: []string{"cl-old"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseCompleted, map[string]any{"test": tc.name})
			committed, err := committer.CommitTerminal(ctx, job.CaseID, entity.CaseStatusDraft, entity.CaseStatusResolved, tc.res, &event)
			if err == nil || committed {
				t.Fatalf("cross-generation %s reference committed=%v err=%v, want rejection", tc.name, committed, err)
			}
			current, getErr := repo.CaseRepo().Get(ctx, job.CaseID)
			if getErr != nil || current.Status != entity.CaseStatusDraft {
				t.Fatalf("failed reference verification changed case: %+v err=%v", current, getErr)
			}
		})
	}

	if err := owned.CreateVoteOwned(ctx, owner2, &entity.Vote{
		ID: "vote-current", CaseID: job.CaseID, Decision: entity.VoteDecisionApprove, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create current vote: %v", err)
	}
	if err := owned.CreateEvidenceOwned(ctx, owner2, &entity.EvidenceRecord{
		ID: "ev-current", CaseID: job.CaseID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create current evidence: %v", err)
	}
	if err := owned.CreateClaimOwned(ctx, owner2, &entity.Claim{
		ID: "cl-current", CaseID: job.CaseID, CreatedAt: now,
	}); err != nil {
		t.Fatalf("create current claim: %v", err)
	}
	res := &entity.Resolution{
		ID: "res-current", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration,
		VoteIDs: []string{"vote-current"}, KeyEvidenceIDs: []string{"ev-current"}, KeyClaimIDs: []string{"cl-current"},
		FinalDecision: entity.VoteDecisionApprove, CreatedAt: now,
	}
	event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseCompleted, nil)
	committed, err := committer.CommitTerminal(ctx, job.CaseID, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || !committed {
		t.Fatalf("same-generation terminal commit = %v err=%v", committed, err)
	}
	stored, err := repo.ResolutionRepo().Get(ctx, job.CaseID)
	if err != nil || stored.ExecutionGeneration != second.ExecutionGeneration {
		t.Fatalf("stored resolution = %+v err=%v", stored, err)
	}
}

func TestMySQLArtifactGeneration_LegacyGenerationZeroRemainsHistory(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo := magi.NewRepository(db)
	ctx := context.Background()
	caseID := "case-t3-legacy-history"
	if err := repo.CaseRepo().Create(ctx, &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	if err := repo.EvidenceRepo().Create(ctx, &entity.EvidenceRecord{ID: "ev-legacy-zero", CaseID: caseID, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("create legacy evidence: %v", err)
	}
	history, err := repo.EvidenceRepo().ListByCase(ctx, caseID)
	if err != nil || len(history) != 1 || history[0].ExecutionGeneration != 0 {
		t.Fatalf("legacy history = %+v err=%v", history, err)
	}
	owned := repo.(port.OwnedArtifactRepository)
	current, err := owned.ListEvidenceByGeneration(ctx, caseID, 1)
	if err != nil || len(current) != 0 {
		t.Fatalf("legacy generation 0 leaked into generation 1: %+v err=%v", current, err)
	}
}

