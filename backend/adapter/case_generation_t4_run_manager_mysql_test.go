package magi_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type t4SettlementJobs struct {
	port.DecisionJobRepository
	succeeded, failed, finalFailed atomic.Int32
}

func (j *t4SettlementJobs) MarkSucceeded(context.Context, string, string, string, int64) error {
	j.succeeded.Add(1)
	return errors.New("separate success settlement must not run")
}
func (j *t4SettlementJobs) MarkFailed(ctx context.Context, job, caseID, worker string, generation int64, reason string, retry *time.Time) error {
	j.failed.Add(1)
	return j.DecisionJobRepository.MarkFailed(ctx, job, caseID, worker, generation, reason, retry)
}
func (j *t4SettlementJobs) CommitFinalFailure(ctx context.Context, job, worker string, generation int64, caseID string, expected []entity.CaseStatus, reason string, event *entity.MagiEvent) (bool, error) {
	j.finalFailed.Add(1)
	return j.DecisionJobRepository.CommitFinalFailure(ctx, job, worker, generation, caseID, expected, reason, event)
}

type t4SettlementOrchestrator struct {
	repo             port.Repository
	waitForHeartbeat bool
	done             chan struct{}
	owner            *entity.ExecutionContext
	resolution       *entity.Resolution
	event            entity.MagiEvent
	calls            atomic.Int32
	observed         atomic.Bool
}

func (o *t4SettlementOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	return nil, port.ErrLeaseLost
}
func (o *t4SettlementOrchestrator) OrchestrateForExecution(ctx context.Context, c *entity.DecisionCase, owner *entity.ExecutionContext) (*entity.Resolution, error) {
	defer close(o.done)
	o.calls.Add(1)
	o.owner, o.resolution, o.event = owner, t4Resolution(owner), t4Completion(owner, entity.CaseStatusResolved)
	committed, err := o.repo.(port.OwnedCaseCommitter).CommitTerminalOwned(ctx, owner, c.Status, entity.CaseStatusResolved, o.resolution, &o.event)
	if err != nil {
		return nil, err
	}
	if !committed {
		return nil, port.ErrLeaseLost
	}
	c.Status = entity.CaseStatusResolved
	if o.waitForHeartbeat {
		// The Job is already settled. Its next heartbeat fails its active
		// owner predicate and cancels ctx before Orchestrate returns success.
		select {
		case <-ctx.Done():
			o.observed.Store(errors.Is(context.Cause(ctx), port.ErrLeaseLost))
		case <-time.After(3 * time.Second):
			return nil, errors.New("heartbeat did not observe settled Job")
		}
	}
	return o.resolution, nil
}

type t4LiveCounter struct{ count atomic.Int32 }

func (p *t4LiveCounter) PublishLive(context.Context, entity.MagiEvent) error {
	p.count.Add(1)
	return nil
}

func TestCaseGeneration_RunManagerSettlementAndUnknownOutcomeOnMySQL(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "heartbeat_after_settlement"
		if unknown {
			name = "unknown_not_retried_or_failed"
		}
		t.Run(name, func(t *testing.T) {
			db := openArtifactGenerationMySQL(t)
			repo, baseJobs, job := seedArtifactGenerationJob(t, db)
			jobs := &t4SettlementJobs{DecisionJobRepository: baseJobs}
			writeRepo := repo
			if unknown {
				fault, _ := t4FaultDB(t, db, true, nil)
				writeRepo = magi.NewRepository(fault)
			}
			orch := &t4SettlementOrchestrator{repo: writeRepo, waitForHeartbeat: !unknown, done: make(chan struct{})}
			reg, publisher := metrics.New(), &t4LiveCounter{}
			manager := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: repo.(port.OwnedCaseCommitter), WorkerID: "settlement-worker", LeaseDuration: time.Second, MaxAttempts: 3, Metrics: reg, LiveEvents: publisher})
			t.Cleanup(manager.Shutdown)
			c, err := repo.CaseRepo().Get(context.Background(), job.CaseID)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Start(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			select {
			case <-orch.done:
			case <-time.After(5 * time.Second):
				t.Fatal("orchestration did not finish")
			}
			if !manager.WaitStopped(job.CaseID, time.Second) {
				t.Fatal("worker did not stop")
			}
			if jobs.succeeded.Load() != 0 || jobs.failed.Load() != 0 || jobs.finalFailed.Load() != 0 || orch.calls.Load() != 1 || publisher.count.Load() != 0 {
				t.Fatalf("settlement replayed: success=%d retry=%d finalFailure=%d calls=%d live=%d", jobs.succeeded.Load(), jobs.failed.Load(), jobs.finalFailed.Load(), orch.calls.Load(), publisher.count.Load())
			}
			if !unknown && !orch.observed.Load() {
				t.Fatal("test never observed heartbeat lease loss after settlement")
			}
			if !unknown && (reg.RunsCompleted.Load() != 1 || reg.RunsFailed.Load() != 0) {
				t.Fatalf("confirmed settlement counted as failure: completed=%d failed=%d", reg.RunsCompleted.Load(), reg.RunsFailed.Load())
			}
			assertT4State(t, db, orch.owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{orch.resolution.ID}, []string{orch.event.ID})
		})
	}
}

func TestCaseGeneration_OrdinaryTransitionReplyLossOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "status-reply-loss")
	fault, pool := t4FaultDB(t, db, false, nil)
	event := entity.NewEvent(owner.CaseID, "", nil, entity.EventCaseStatusChanged, map[string]any{"status": string(entity.CaseStatusInvestigating)})
	event.ExecutionGeneration = owner.ExecutionGeneration
	committed, err := t4Committer(magi.NewRepository(fault)).CommitStatusTransitionOwned(context.Background(), owner, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusInvestigating, &event)
	if err != nil || !committed || !pool.lost.Load() || event.Seq != 1 {
		t.Fatalf("ordinary reply loss=%v err=%v event=%+v", committed, err, event)
	}
	assertT4State(t, db, owner, entity.CaseStatusInvestigating, entity.DecisionJobRunning, nil, []string{event.ID})
	committed, err = t4Committer(repo).CommitStatusTransitionOwned(context.Background(), owner, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusInvestigating, &event)
	if err != nil || committed {
		t.Fatalf("ordinary replay=%v err=%v", committed, err)
	}
	assertT4State(t, db, owner, entity.CaseStatusInvestigating, entity.DecisionJobRunning, nil, []string{event.ID})
}
