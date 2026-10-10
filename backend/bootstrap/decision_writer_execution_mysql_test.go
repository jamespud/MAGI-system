package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
)

type t6OwnedOrchestrator struct {
	repo   port.Repository
	called chan *entity.ExecutionContext
}

func (o *t6OwnedOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	return nil, port.ErrExecutionOwnerRequired
}
func (o *t6OwnedOrchestrator) OrchestrateForExecution(ctx context.Context, c *entity.DecisionCase, owner *entity.ExecutionContext) (*entity.Resolution, error) {
	o.called <- owner
	owned := o.repo.(port.OwnedCaseCommitter)
	status := entity.NewEvent(c.ID, "", nil, entity.EventCaseStatusChanged, nil)
	status.ExecutionGeneration = owner.ExecutionGeneration
	ok, err := owned.CommitStatusTransitionOwned(ctx, owner, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusResolving, &status)
	if err != nil || !ok {
		return nil, err
	}
	res := &entity.Resolution{ID: uuid.NewString(), CaseID: c.ID, ExecutionGeneration: owner.ExecutionGeneration, FinalDecision: entity.VoteDecisionApprove}
	event := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
	event.ExecutionGeneration = owner.ExecutionGeneration
	ok, err = owned.CommitTerminalOwned(ctx, owner, entity.CaseStatusResolving, entity.CaseStatusResolved, res, &event)
	if err != nil || !ok {
		return nil, err
	}
	return res, nil
}

func TestMySQLDecisionWriter_OwnerlessAndBenchmark(t *testing.T) {
	_, db := t6SchemaFixture(t)
	repo := magi.NewDecisionWriterRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	if err := repo.CaseRepo().Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{Repo: repo})
	if _, err := orch.Orchestrate(ctx, c); !errors.Is(err, port.ErrExecutionOwnerRequired) {
		t.Fatalf("ownerless orchestrator: %v", err)
	}
	service := decision.NewService(orch, decision.ServiceConfig{}, decision.WithCaseRepo(repo.CaseRepo()))
	if err := service.StartRun(ctx, c); !errors.Is(err, port.ErrExecutionOwnerRequired) {
		t.Fatalf("sync fallback: %v", err)
	}
	for _, write := range []func() error{
		func() error { return repo.CaseRepo().UpdateStatus(ctx, c.ID, entity.CaseStatusResolved) },
		func() error { return repo.VoteRepo().Create(ctx, &entity.Vote{ID: uuid.NewString(), CaseID: c.ID}) },
		func() error {
			e := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
			return repo.EventRepo().Create(ctx, &e)
		},
		func() error {
			return repo.CheckpointRepo().Save(ctx, &entity.AgentState{RunID: "legacy", CaseID: c.ID})
		},
	} {
		if err := write(); !errors.Is(err, port.ErrExecutionOwnerRequired) {
			t.Fatalf("legacy write: %v", err)
		}
	}
	jobs := magi.NewDecisionJobRepository(db)
	fake := &t6OwnedOrchestrator{repo: repo, called: make(chan *entity.ExecutionContext, 1)}
	rm := decision.NewRunManager(fake, decision.RunManagerDeps{JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: repo.(port.OwnedCaseCommitter), Metrics: metrics.New()})
	defer rm.Shutdown()
	runner := &benchmarkExecutor{runs: rm, repo: repo, jobs: jobs}
	res, err := runner.Orchestrate(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	owner := <-fake.called
	fresh, _ := repo.CaseRepo().Get(ctx, c.ID)
	job, _ := jobs.GetByCase(ctx, c.ID)
	if res.ExecutionGeneration != 1 || owner.ExecutionGeneration != 1 || fresh.ExecutionGeneration != 1 || fresh.Status != entity.CaseStatusResolved || job.Status != entity.DecisionJobSucceeded {
		t.Fatal("benchmark bypassed durable lifecycle")
	}
	events, _ := repo.EventRepo().ListByCase(ctx, c.ID)
	if len(events) != 2 {
		t.Fatalf("events: %d", len(events))
	}
	for _, e := range events {
		if e.ExecutionGeneration != 1 {
			t.Fatal("generation-zero benchmark event")
		}
	}
}

func TestMySQLDecisionWriter_HistoryAndBlockedRecovery(t *testing.T) {
	_, db := t6SchemaFixture(t)
	ctx := context.Background()
	repo := magi.NewDecisionWriterRepository(db)
	legacy := magi.NewRepository(db)
	historical := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusResolved}
	pending := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
	for _, c := range []*entity.DecisionCase{historical, pending} {
		if err := legacy.CaseRepo().Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	vote := &entity.Vote{ID: uuid.NewString(), CaseID: pending.ID}
	if err := legacy.VoteRepo().Create(ctx, vote); err != nil {
		t.Fatal(err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	job, _, err := jobs.Admit(ctx, pending.ID, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = BlockDecisionWriter(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	fake := &t6OwnedOrchestrator{repo: repo, called: make(chan *entity.ExecutionContext, 1)}
	rm := decision.NewRunManager(fake, decision.RunManagerDeps{JobRepo: jobs, CaseRepo: repo.CaseRepo()})
	defer rm.Shutdown()
	if err = rm.Recover(ctx); !errors.Is(err, port.ErrDecisionWriterBlocked) {
		t.Fatal("recovery passed blocked gate")
	}
	select {
	case <-fake.called:
		t.Fatal("orchestration launched before gate")
	default:
	}
	current, _ := jobs.GetByCase(ctx, pending.ID)
	if current.Status != entity.DecisionJobQueued || current.ExecutionGeneration != 0 {
		t.Fatal("blocked recovery mutated job")
	}
	read, err := repo.CaseRepo().Get(ctx, historical.ID)
	if err != nil || read.Status != entity.CaseStatusResolved || read.ExecutionGeneration != 0 {
		t.Fatal("legacy history unreadable")
	}
	// Test-only activation: the integrated cutover tests exercise the real admin.
	if err = db.Exec("UPDATE decision_writer_contract SET admission_state='ENABLED'").Error; err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := jobs.Claim(ctx, job.ID, "w", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || claimed.ExecutionGeneration != 1 {
		t.Fatalf("first claim: %v", err)
	}
	currentVotes, err := repo.(port.OwnedArtifactRepository).ListVotesByGeneration(ctx, pending.ID, 1)
	if err != nil || len(currentVotes) != 0 {
		t.Fatal("G1 consumed G0 history")
	}
	history, err := repo.VoteRepo().ListByCase(ctx, pending.ID)
	if err != nil || len(history) != 1 || history[0].ExecutionGeneration != 0 {
		t.Fatal("G0 history lost")
	}
}
