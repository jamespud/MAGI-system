package orchestration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
)

// durableArtifactRepo upgrades the in-memory stubRepo with the capability T3
// requires of a durable worker: generation-scoped authoritative writes behind
// the repository's active-owner predicate. One artifact kind can be made to
// return a non-lease storage error, modelling the failures that used to be
// logged and swallowed (deadlock, lock-wait timeout, INSERT error, an
// unknown commit outcome).
type durableArtifactRepo struct {
	*stubRepo
	failKind string
	failErr  error
	writes   []string
}

func (r *durableArtifactRepo) recordOwned(kind string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, kind)
	if kind == r.failKind && r.failErr != nil {
		return r.failErr
	}
	return nil
}

func (r *durableArtifactRepo) CreateAgentRunOwned(_ context.Context, _ *entity.ExecutionContext, a *entity.AgentRun) error {
	if err := r.recordOwned("agent_run"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentRuns = append(r.agentRuns, a)
	return nil
}

func (r *durableArtifactRepo) CreateEvidenceOwned(_ context.Context, _ *entity.ExecutionContext, e *entity.EvidenceRecord) error {
	if err := r.recordOwned("evidence"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.evidence = append(r.evidence, e)
	return nil
}

func (r *durableArtifactRepo) CreateClaimOwned(_ context.Context, _ *entity.ExecutionContext, c *entity.Claim) error {
	if err := r.recordOwned("claim"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims = append(r.claims, c)
	return nil
}

func (r *durableArtifactRepo) CreateVoteOwned(_ context.Context, _ *entity.ExecutionContext, v *entity.Vote) error {
	if err := r.recordOwned("vote"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.votes = append(r.votes, v)
	return nil
}

func (r *durableArtifactRepo) CreateDebateRoundOwned(_ context.Context, _ *entity.ExecutionContext, _ *entity.DebateRound) error {
	return r.recordOwned("debate_round")
}

func (r *durableArtifactRepo) CreateReflectionOwned(_ context.Context, _ *entity.ExecutionContext, _ *entity.Reflection) error {
	return r.recordOwned("reflection")
}

func (r *durableArtifactRepo) CreateToolCallOwned(_ context.Context, _ *entity.ExecutionContext, tc *entity.ToolCall) error {
	if err := r.recordOwned("tool_call"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCalls = append(r.toolCalls, tc)
	return nil
}

func (r *durableArtifactRepo) ListAgentRunsByGeneration(context.Context, string, int64) ([]*entity.AgentRun, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListEvidenceByGeneration(context.Context, string, int64) ([]*entity.EvidenceRecord, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListClaimsByGeneration(context.Context, string, int64) ([]*entity.Claim, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListVotesByGeneration(context.Context, string, int64) ([]*entity.Vote, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListDebateRoundsByGeneration(context.Context, string, int64) ([]*entity.DebateRound, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListReflectionsByGeneration(context.Context, string, int64) ([]*entity.Reflection, error) {
	return nil, nil
}
func (r *durableArtifactRepo) ListToolCallsByGeneration(context.Context, string, int64) ([]*entity.ToolCall, error) {
	return nil, nil
}

var _ port.OwnedArtifactRepository = (*durableArtifactRepo)(nil)

// P1: on the positive-generation durable path a repository/storage error is not
// a tolerated metric, it aborts the execution. Before this fix every tier below
// the vote/evidence/claim reference fence (AgentRun, ToolCall, Reflection,
// DebateRound) could fail and the case still reported success.
func TestOrchestrateForExecution_DurableArtifactFailureAbortsExecution(t *testing.T) {
	storageErr := errors.New("mysql: transient storage failure")
	for _, kind := range []string{"agent_run", "evidence", "claim", "tool_call", "vote"} {
		t.Run(kind, func(t *testing.T) {
			repo := &durableArtifactRepo{stubRepo: newStubRepo(), failKind: kind, failErr: storageErr}
			reg := metrics.New()
			mrt := newMockMagiRuntime()
			mrt.votes["melchior"] = []*entity.Vote{approve(), approve()}
			mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
			mrt.votes["casper"] = []*entity.Vote{approve(), approve()}

			orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
				Repo:      repo,
				CaseRepo:  repo.CaseRepo(),
				AgentLoop: mrt,
				Consensus: consensus.NewConsensusEngine(),
				Debate:    debate.NewDebateEngine(nil),
				Commander: newCommander(t),
				Configs: []*entity.MagiConfig{
					magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper"),
				},
				Policy:  consensus.DefaultConsensusPolicy(),
				Metrics: reg,
			})

			case_ := &entity.DecisionCase{
				ID: "c-durable-fail", Question: "compute", MaxDebateRounds: 1,
				ExecutionGeneration: 5, ExecutionAttempt: 2,
			}
			owner := &entity.ExecutionContext{
				CaseID: case_.ID, JobID: "job-1", WorkerID: "worker-1",
				JobAttempt: 2, ExecutionGeneration: 5,
			}

			res, err := orch.OrchestrateForExecution(context.Background(), case_, owner)
			if err == nil {
				t.Fatalf("durable %s write failure was swallowed; resolution=%+v", kind, res)
			}
			if errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("storage error was misreported as lease loss: %v", err)
			}
			if !errors.Is(err, storageErr) {
				t.Fatalf("error = %v, want the injected storage error to survive", err)
			}
			if got := reg.ArtifactPersistFailures(metrics.ArtifactKind(kind)); got == 0 {
				t.Fatalf("artifact persist failure for %s was not counted", kind)
			}
			// The execution must not advertise a successful terminal outcome.
			switch repo.statuses[case_.ID] {
			case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
				entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
				t.Fatalf("failed durable execution reached success terminal %s", repo.statuses[case_.ID])
			}
			repo.mu.Lock()
			defer repo.mu.Unlock()
			if len(repo.resolutions) != 0 {
				t.Fatalf("failed durable execution persisted %d resolutions", len(repo.resolutions))
			}
		})
	}
}

// A durable execution whose authoritative write fails must stop at the failing
// artifact, not keep writing later artifacts that would then look like a
// complete generation.
func TestOrchestrateForExecution_DurableArtifactFailureStopsGeneration(t *testing.T) {
	storageErr := errors.New("mysql: insert into magi_agent_run failed")
	repo := &durableArtifactRepo{stubRepo: newStubRepo(), failKind: "agent_run", failErr: storageErr}
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve(), approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{approve(), approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		Repo: repo, CaseRepo: repo.CaseRepo(), AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(), Debate: debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
		Metrics:   metrics.New(),
	})
	case_ := &entity.DecisionCase{
		ID: "c-durable-stop", Question: "compute", MaxDebateRounds: 1,
		ExecutionGeneration: 3, ExecutionAttempt: 1,
	}
	owner := &entity.ExecutionContext{
		CaseID: case_.ID, JobID: "job-1", WorkerID: "worker-1", ExecutionGeneration: 3,
	}

	if _, err := orch.OrchestrateForExecution(context.Background(), case_, owner); !errors.Is(err, storageErr) {
		t.Fatalf("error = %v, want the injected agent-run storage error", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.writes) != 1 || repo.writes[0] != "agent_run" {
		t.Fatalf("authoritative writes after the failure = %v, want exactly one agent_run attempt", repo.writes)
	}
	if len(repo.votes) != 0 || len(repo.evidence) != 0 || len(repo.claims) != 0 {
		t.Fatalf("generation continued past the failed write: votes=%d evidence=%d claims=%d",
			len(repo.votes), len(repo.evidence), len(repo.claims))
	}
}
