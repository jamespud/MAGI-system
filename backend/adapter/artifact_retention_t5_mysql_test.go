package magi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

// Keep the retired signature local to the regression test. Production must
// expose no case-wide cleanup capability, including through a type assertion.
type t5LegacyCleaner interface {
	CleanupCaseArtifacts(context.Context, string) error
}

type t5Artifacts struct {
	run, evidence, claim, vote, debate, reflection, tool, checkpoint string
}

func writeT5Artifacts(ctx context.Context, repo port.Repository, owner *entity.ExecutionContext) (t5Artifacts, error) {
	ids := t5Artifacts{uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), "checkpoint-" + owner.CaseID}
	owned := repo.(port.OwnedArtifactRepository)
	now := time.Now()
	writes := []func() error{
		func() error {
			return owned.CreateAgentRunOwned(ctx, owner, &entity.AgentRun{ID: ids.run, CaseID: owner.CaseID, StartedAt: now, Usage: &entity.Usage{TotalTokens: 100, CostUSD: 0.25}})
		},
		func() error {
			return owned.CreateEvidenceOwned(ctx, owner, &entity.EvidenceRecord{ID: ids.evidence, CaseID: owner.CaseID, AgentRunID: ids.run, RawContent: "retained evidence", CreatedAt: now})
		},
		func() error {
			return owned.CreateClaimOwned(ctx, owner, &entity.Claim{ID: ids.claim, CaseID: owner.CaseID, AgentRunID: ids.run, Supports: []string{ids.evidence}, CreatedAt: now})
		},
		func() error {
			return owned.CreateVoteOwned(ctx, owner, &entity.Vote{ID: ids.vote, CaseID: owner.CaseID, AgentRunID: ids.run, Decision: entity.VoteDecisionApprove, CreatedAt: now})
		},
		func() error {
			return owned.CreateDebateRoundOwned(ctx, owner, &entity.DebateRound{ID: ids.debate, CaseID: owner.CaseID, StartedAt: now})
		},
		func() error {
			return owned.CreateReflectionOwned(ctx, owner, &entity.Reflection{ID: ids.reflection, CaseID: owner.CaseID, AgentRunID: ids.run, CreatedAt: now})
		},
		func() error {
			return owned.CreateToolCallOwned(ctx, owner, &entity.ToolCall{ID: ids.tool, CaseID: owner.CaseID, AgentRunID: ids.run, EvidenceID: ids.evidence, CreatedAt: now})
		},
		func() error {
			return repo.(port.GenerationCheckpointRepository).SaveForExecution(ctx, owner, &entity.AgentState{RunID: ids.checkpoint, CaseID: owner.CaseID, StepCount: int(owner.ExecutionGeneration), Phase: fmt.Sprint(owner.ExecutionGeneration)})
		},
	}
	for _, write := range writes {
		if err := write(); err != nil {
			return ids, err
		}
	}
	state, err := repo.(port.GenerationCheckpointRepository).LoadForExecution(ctx, owner, ids.checkpoint)
	if err != nil {
		return ids, err
	}
	if state == nil || state.ExecutionGeneration != owner.ExecutionGeneration || state.CaseID != owner.CaseID || state.StepCount != int(owner.ExecutionGeneration) {
		return ids, fmt.Errorf("checkpoint loaded a different generation: %+v", state)
	}
	return ids, nil
}

func commitT5Artifacts(ctx context.Context, repo port.Repository, owner *entity.ExecutionContext, ids t5Artifacts) (*entity.Resolution, entity.MagiEvent, error) {
	res := t4Resolution(owner)
	res.VoteIDs, res.KeyEvidenceIDs, res.KeyClaimIDs = []string{ids.vote}, []string{ids.evidence}, []string{ids.claim}
	event := t4Completion(owner, entity.CaseStatusResolved)
	ok, err := t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err == nil && !ok {
		err = port.ErrLeaseLost
	}
	return res, event, err
}

// Snapshot full rows rather than counts: a delete/reinsert, cursor advance or
// provenance rewrite is also a violation. This includes another Case and
// legacy rows with unknown Case provenance, without inferring it from RunID.
func snapshotT5(t *testing.T, db *gorm.DB) map[string][]map[string]any {
	t.Helper()
	out := make(map[string][]map[string]any)
	for _, table := range []string{"decision_case", "decision_job", "decision_job_claim", "magi_agent_run", "evidence_record", "claim", "magi_vote", "debate_round", "reflection", "magi_tool_call", "magi_agent_checkpoint", "resolution", "magi_event", "magi_event_cursor"} {
		order := "id"
		switch table {
		case "decision_job_claim":
			order = "claim_token"
		case "magi_agent_checkpoint":
			order = "run_id, execution_generation"
		case "magi_event_cursor":
			order = "case_id"
		}
		var rows []map[string]any
		if err := db.Table(table).Order(order).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		out[table] = rows
	}
	return out
}

func TestArtifactRetention_LegacyCleanupCannotDeleteNewGenerationOnMySQL(t *testing.T) {
	for _, delayed := range []bool{true, false} {
		name := "stale_request_after_takeover"
		if delayed {
			name = "delayed_request_after_terminal_commit"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			first, old := claimArtifactOwner(t, jobs, job, "retention-worker")
			if _, err := writeT5Artifacts(ctx, repo, old); err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			type result struct {
				available bool
				err       error
			}
			done := make(chan result, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			launched, finished := false, false
			defer func() {
				unblock()
				if launched && !finished {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("cleanup request did not stop before fixture teardown")
					}
				}
			}()
			attempt := func() {
				cleaner, available := repo.(t5LegacyCleaner)
				close(started)
				<-release
				var err error
				if available {
					cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					err = cleaner.CleanupCaseArtifacts(cleanupCtx, old.CaseID)
					cancel()
				}
				done <- result{available, err}
			}
			if delayed {
				launched = true
				go attempt()
				<-started
			}
			requeueArtifactOwner(t, jobs, first, old.WorkerID)
			_, current := claimArtifactOwner(t, jobs, job, old.WorkerID)
			ids, err := writeT5Artifacts(ctx, repo, current)
			if err != nil {
				t.Fatal(err)
			}
			var res *entity.Resolution
			var event entity.MagiEvent
			if delayed {
				res, event, err = commitT5Artifacts(ctx, repo, current, ids)
				if err != nil {
					t.Fatal(err)
				}
			}

			// A second Case has both generations, so broad maintenance cannot
			// silently affect its current or historical records either.
			otherRepo, otherJobs, otherJob := seedArtifactGenerationJob(t, db)
			otherFirst, otherOld := claimArtifactOwner(t, otherJobs, otherJob, "other-worker")
			if _, err := writeT5Artifacts(ctx, otherRepo, otherOld); err != nil {
				t.Fatal(err)
			}
			requeueArtifactOwner(t, otherJobs, otherFirst, otherOld.WorkerID)
			_, otherOwner := claimArtifactOwner(t, otherJobs, otherJob, otherOld.WorkerID)
			if _, err := writeT5Artifacts(ctx, otherRepo, otherOwner); err != nil {
				t.Fatal(err)
			}
			before := snapshotT5(t, db)
			if !delayed {
				launched = true
				go attempt()
				<-started
			}
			unblock()
			select {
			case got := <-done:
				finished = true
				if got.available {
					t.Errorf("retired cleanup capability remains callable: %v", got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup request did not finish")
			}
			after := snapshotT5(t, db)
			for table, rows := range before {
				if !reflect.DeepEqual(rows, after[table]) {
					t.Errorf("cleanup changed %s", table)
				}
			}
			if !delayed {
				assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
				res, event, err = commitT5Artifacts(ctx, repo, current, ids)
				if err != nil {
					t.Fatal(err)
				}
			}
			assertT4State(t, db, current, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{event.ID})
		})
	}
}

type t5RetryOrchestrator struct {
	repo       port.Repository
	owners     []*entity.ExecutionContext
	artifacts  []t5Artifacts
	resolution *entity.Resolution
	event      entity.MagiEvent
}

func (o *t5RetryOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	return nil, port.ErrLeaseLost
}
func (o *t5RetryOrchestrator) OrchestrateForExecution(ctx context.Context, c *entity.DecisionCase, owner *entity.ExecutionContext) (*entity.Resolution, error) {
	ids, err := writeT5Artifacts(ctx, o.repo, owner)
	if err != nil {
		return nil, err
	}
	o.owners, o.artifacts = append(o.owners, owner), append(o.artifacts, ids)
	if len(o.owners) == 1 {
		return nil, errors.New("retry with all artifacts retained")
	}
	o.resolution, o.event, err = commitT5Artifacts(ctx, o.repo, owner, ids)
	if err == nil {
		c.Status = entity.CaseStatusResolved
	}
	return o.resolution, err
}

func TestArtifactRetention_NoCleanupRetryAndHistoryOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	// Legacy unknown rows deliberately resemble this Case's old run naming.
	// Retention must neither infer their provenance nor remove them.
	unknown := job.CaseID + "-a1-unknown"
	legacy := []any{
		&magi.ReflectionModel{ID: unknown, AgentRunID: unknown},
		&magi.ToolCallModel{ID: unknown, AgentRunID: unknown},
		&magi.CheckpointModel{RunID: unknown, Phase: "legacy"},
	}
	for _, row := range legacy {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = db.Where("id = ?", unknown).Delete(&magi.ReflectionModel{}).Error
		_ = db.Where("id = ?", unknown).Delete(&magi.ToolCallModel{}).Error
		_ = db.Where("run_id = ?", unknown).Delete(&magi.CheckpointModel{}).Error
	})
	before := snapshotT5(t, db)
	orch := &t5RetryOrchestrator{repo: repo}
	manager := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: t4Committer(repo), WorkerID: "no-cleanup-worker", MaxAttempts: 2, RetryBase: time.Millisecond})
	t.Cleanup(manager.Shutdown)
	c, err := repo.CaseRepo().Get(ctx, job.CaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Start(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !manager.WaitStopped(job.CaseID, 5*time.Second) {
		t.Fatal("retry did not stop")
	}
	if len(orch.owners) != 2 || orch.owners[0].ExecutionGeneration != 1 || orch.owners[1].ExecutionGeneration != 2 || orch.resolution == nil {
		t.Fatalf("retry owners=%+v resolution=%+v", orch.owners, orch.resolution)
	}
	owner, ids := orch.owners[1], orch.artifacts[1]
	assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{orch.resolution.ID}, []string{orch.event.ID})
	owned := repo.(port.OwnedArtifactRepository)
	checks := []struct {
		name    string
		current func(int64) (any, error)
		history func() (any, error)
		id      string
	}{
		{"run", func(g int64) (any, error) { return owned.ListAgentRunsByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.AgentRunRepo().ListByCase(ctx, job.CaseID) }, ids.run},
		{"evidence", func(g int64) (any, error) { return owned.ListEvidenceByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.EvidenceRepo().ListByCase(ctx, job.CaseID) }, ids.evidence},
		{"claim", func(g int64) (any, error) { return owned.ListClaimsByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.ClaimRepo().ListByCase(ctx, job.CaseID) }, ids.claim},
		{"vote", func(g int64) (any, error) { return owned.ListVotesByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.VoteRepo().ListByCase(ctx, job.CaseID) }, ids.vote},
		{"debate", func(g int64) (any, error) { return owned.ListDebateRoundsByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.DebateRepo().ListByCase(ctx, job.CaseID) }, ids.debate},
		{"reflection", func(g int64) (any, error) { return owned.ListReflectionsByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.ReflectionRepo().ListByCase(ctx, job.CaseID) }, ids.reflection},
		{"tool", func(g int64) (any, error) { return owned.ListToolCallsByGeneration(ctx, job.CaseID, g) }, func() (any, error) { return repo.ToolCallRepo().ListByCase(ctx, job.CaseID) }, ids.tool},
	}
	for _, check := range checks {
		rows, err := check.current(2)
		v := reflect.ValueOf(rows)
		if err != nil || v.Len() != 1 {
			t.Fatalf("%s current=%+v err=%v", check.name, rows, err)
		}
		if id := v.Index(0).Elem().FieldByName("ID").String(); id != check.id {
			t.Fatalf("%s current id=%s want=%s", check.name, id, check.id)
		}
		rows, err = check.history()
		v = reflect.ValueOf(rows)
		if err != nil || v.Len() != 2 {
			t.Fatalf("%s history=%+v err=%v", check.name, rows, err)
		}
		generations := make(map[int64]bool)
		for i := 0; i < v.Len(); i++ {
			row := v.Index(i).Elem()
			if row.FieldByName("CaseID").String() != job.CaseID {
				t.Fatal("history crossed Case boundary")
			}
			g := row.FieldByName("ExecutionGeneration").Int()
			generations[g] = true
			if f := row.FieldByName("AgentRunID"); f.IsValid() && (g < 1 || g > 2 || f.String() != orch.artifacts[g-1].run) {
				t.Fatalf("%s orphaned relationship: %+v", check.name, rows)
			}
		}
		if !generations[1] || !generations[2] {
			t.Fatalf("%s history generations=%v", check.name, generations)
		}
	}
	// Loading a checkpoint requires an active owner, so verify the retained
	// snapshots directly after terminal settlement has released its owner.
	var states []magi.CheckpointModel
	if err := db.Where("run_id = ?", ids.checkpoint).Order("execution_generation").Find(&states).Error; err != nil || len(states) != 2 {
		t.Fatalf("checkpoints=%+v err=%v", states, err)
	}
	for i, state := range states {
		if state.CaseID != job.CaseID || state.ExecutionGeneration != int64(i+1) || state.StepCount != i+1 {
			t.Fatalf("checkpoint=%+v", state)
		}
	}
	after := snapshotT5(t, db)
	for _, table := range []string{"reflection", "magi_tool_call", "magi_agent_checkpoint"} {
		for _, old := range before[table] {
			found := false
			for _, row := range after[table] {
				if reflect.DeepEqual(old, row) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("legacy unknown row changed in %s: %+v", table, old)
			}
		}
	}
	runs, err := repo.AgentRunRepo().ListByCase(ctx, job.CaseID)
	if err != nil || len(runs) != 2 {
		t.Fatalf("retained runs=%+v err=%v", runs, err)
	}
	for _, run := range runs {
		if run.Usage == nil || run.Usage.TotalTokens != 100 || run.Usage.CostUSD != 0.25 {
			t.Fatalf("lost billing history: %+v", run)
		}
	}
}
