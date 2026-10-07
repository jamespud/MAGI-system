package magi_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
)

// --- minimal orchestration harness for the durable MySQL path ---

type t3ScriptedModel struct {
	mu        sync.Mutex
	responses []*schema.Message
	calls     int
}

func (m *t3ScriptedModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls >= len(m.responses) {
		return nil, fmt.Errorf("t3 scripted model exhausted after %d calls", m.calls)
	}
	msg := m.responses[m.calls]
	m.calls++
	return msg, nil
}

func (m *t3ScriptedModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stream not implemented")
}

func (m *t3ScriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

type t3ModelPort struct{ m model.ToolCallingChatModel }

func (p t3ModelPort) Build(context.Context, entity.ModelRef) (model.ToolCallingChatModel, error) {
	return p.m, nil
}

// t3Runtime returns one approving ballot per agent without touching a model.
// The ledger and trace are non-empty so the orchestrator exercises the
// evidence/claim/tool-call writes as well as the agent run and vote.
type t3Runtime struct{}

func (t3Runtime) Run(_ context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	ledger := evidence.NewEvidenceLedger(actx.CaseID, "", cfg.Code)
	ledger.Record("tc", "tool", "local", "", "observation", entity.ReliabilityScore{Final: 0.9})
	ledger.RecordClaim("claim by "+cfg.Code, nil, nil)
	return &runtime.LoopResult{
		Vote:   &entity.Vote{Decision: entity.VoteDecisionApprove, Confidence: 90, EvidenceIDs: []string{"EV-001"}},
		Status: runtime.LoopStatusCompleted,
		Ledger: ledger,
		Trace: &runtime.LoopTrace{Steps: []*runtime.Step{{
			IsFinal: true,
			ToolCalls: []runtime.ToolCallRecord{{
				ToolCallID: "call-1", ToolName: "calc", Arguments: `{"a":1,"b":2}`,
				Valid: true, Result: "3", Duration: 5 * time.Millisecond,
			}},
		}}},
		Usage: &entity.Usage{TotalTokens: 100},
	}, nil
}

func t3MagiConfig(code string) *entity.MagiConfig {
	return &entity.MagiConfig{
		Code: code, Persona: code,
		Objective: entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{
			{Code: "correctness", Weight: 0.5, Description: "be correct"},
		}},
		RiskTendency: entity.RiskTendencyNeutral,
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount: 1, MinQuantitativeCount: 1, MinReliability: 0,
			RequireOwnCollected: true,
			RequiredTypes:       []entity.EvidenceTypeRequirement{{Type: "quantitative", MinCount: 1}},
		},
		Model:      entity.ModelRef{ModelID: 1},
		Tools:      []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
		LoopPolicy: entity.LoopPolicy{MaxSteps: 12},
	}
}

func t3Commander(t *testing.T) *service.Commander {
	t.Helper()
	script := &t3ScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"canonical_question":"compute"}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"decision report summary","key_reasons":["r1"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd, err := service.NewCommander(
		service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"},
		t3ModelPort{m: script},
		validation.NewReflectSchemaGenerator(),
		validation.NewJSONSchemaValidator(),
	)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}
	return cmd
}

// t3DurableCase claims a real MySQL decision job and loads the Case row the
// claim just advanced to that execution generation, so the run satisfies the
// repository's active-owner predicate from the first authoritative write.
func t3DurableCase(t *testing.T, db *gorm.DB, repo port.Repository, jobs port.DecisionJobRepository, job *entity.DecisionJob, worker string) (*entity.DecisionCase, *entity.ExecutionContext) {
	t.Helper()
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", job.CaseID).
		Updates(map[string]any{"question": "compute", "max_debate_rounds": 1}).Error; err != nil {
		t.Fatalf("seed case content: %v", err)
	}
	claimed, owner := claimArtifactOwner(t, jobs, job, worker)
	case_, err := repo.CaseRepo().Get(context.Background(), job.CaseID)
	if err != nil || case_ == nil {
		t.Fatalf("load claimed case: %+v err=%v", case_, err)
	}
	if case_.ExecutionGeneration != owner.ExecutionGeneration {
		t.Fatalf("case generation %d != owner generation %d", case_.ExecutionGeneration, owner.ExecutionGeneration)
	}
	case_.ExecutionAttempt = claimed.Attempt
	return case_, owner
}

func t3Orchestrator(t *testing.T, repo port.Repository, reg *metrics.Registry) *orchestration.Orchestrator {
	t.Helper()
	return orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		Repo:      repo,
		CaseRepo:  repo.CaseRepo(),
		AgentLoop: t3Runtime{},
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: t3Commander(t),
		Configs: []*entity.MagiConfig{
			t3MagiConfig("melchior"), t3MagiConfig("balthasar"), t3MagiConfig("casper"),
		},
		Policy:  consensus.DefaultConsensusPolicy(),
		Metrics: reg,
	})
}

func t3IsSuccessTerminal(status entity.CaseStatus) bool {
	switch status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed,
		entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

// P1 control: the harness itself reaches a durable RESOLVED case on real MySQL.
// Without this, "the case never reaches success" would be vacuous.
func TestArtifactGeneration_DurableOrchestrationResolvesOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	case_, owner := t3DurableCase(t, db, repo, jobs, job, "worker-orch-ok-"+uuid.NewString())
	reg := metrics.New()

	res, err := t3Orchestrator(t, repo, reg).OrchestrateForExecution(context.Background(), case_, owner)
	if err != nil {
		t.Fatalf("durable orchestration: %v", err)
	}
	if res == nil || res.FinalReport == "" {
		t.Fatalf("resolution = %+v, want a persisted report", res)
	}
	stored, err := repo.CaseRepo().Get(context.Background(), case_.ID)
	if err != nil || stored.Status != entity.CaseStatusResolved {
		t.Fatalf("case status after success = %+v err=%v", stored, err)
	}
	runs, err := repo.(port.OwnedArtifactRepository).ListAgentRunsByGeneration(context.Background(), case_.ID, owner.ExecutionGeneration)
	if err != nil || len(runs) != 3 {
		t.Fatalf("gen%d agent runs = %d err=%v, want 3", owner.ExecutionGeneration, len(runs), err)
	}
	if got := reg.ArtifactPersistFailures(metrics.ArtifactAgentRun); got != 0 {
		t.Fatalf("a successful run counted %d agent-run persist failures", got)
	}
}

// P1: a real MySQL INSERT failure on an authoritative artifact aborts the
// durable execution. The case must not reach a success terminal and must not
// persist a resolution.
func TestArtifactGeneration_DurableOrchestrationAbortsOnArtifactInsertError(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	case_, owner := t3DurableCase(t, db, repo, jobs, job, "worker-orch-insert-"+uuid.NewString())
	reg := metrics.New()

	trigger := "t3_orch_fail_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	ddl := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON magi_agent_run
		FOR EACH ROW
		BEGIN
			SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 't3 orchestration artifact insert failure';
		END`, trigger)
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create insert-failure trigger: %v", err)
	}
	defer func() { _ = db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error }()

	res, err := t3Orchestrator(t, repo, reg).OrchestrateForExecution(context.Background(), case_, owner)
	if err == nil {
		t.Fatalf("durable orchestration swallowed an artifact INSERT failure: %+v", res)
	}
	if errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("artifact INSERT failure was misreported as lease loss: %v", err)
	}
	if !strings.Contains(err.Error(), "t3 orchestration artifact insert failure") {
		t.Fatalf("error = %v, want the injected MySQL failure", err)
	}
	stored, getErr := repo.CaseRepo().Get(context.Background(), case_.ID)
	if getErr != nil {
		t.Fatalf("reload case: %v", getErr)
	}
	if t3IsSuccessTerminal(stored.Status) {
		t.Fatalf("case reached success terminal %s after an authoritative write failed", stored.Status)
	}
	if res != nil {
		t.Fatalf("failed execution returned a resolution: %+v", res)
	}
	var resolutions int64
	if err := db.Model(&magi.ResolutionModel{}).Where("case_id = ?", case_.ID).Count(&resolutions).Error; err != nil {
		t.Fatalf("count resolutions: %v", err)
	}
	if resolutions != 0 {
		t.Fatalf("failed execution persisted %d resolution rows, want 0", resolutions)
	}
	if got := reg.ArtifactPersistFailures(metrics.ArtifactAgentRun); got == 0 {
		t.Fatal("artifact persist failure was not counted")
	}
}

// P1: real MySQL storage error during owner verification (lock wait timeout on
// the decision_job row) aborts the durable execution instead of being logged
// away. Before this fix the run continued and reported a complete decision
// while the authoritative writes it claimed never landed.
func TestArtifactGeneration_DurableOrchestrationAbortsOnOwnerVerificationError(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	case_, owner := t3DurableCase(t, db, repo, jobs, job, "worker-orch-owner-"+uuid.NewString())
	reg := metrics.New()
	singleConnPool(t, db)
	if err := db.Exec("SET SESSION innodb_lock_wait_timeout = 1").Error; err != nil {
		t.Fatalf("set lock wait timeout: %v", err)
	}

	blocker := openA2AMySQL(t)
	blockerTx := blocker.Begin()
	defer func() { _ = blockerTx.Rollback().Error }()
	var lockedID string
	if err := blockerTx.Raw("SELECT id FROM decision_job WHERE id = ? FOR UPDATE", job.ID).Scan(&lockedID).Error; err != nil {
		t.Fatalf("hold decision_job lock: %v", err)
	}

	res, err := t3Orchestrator(t, repo, reg).OrchestrateForExecution(context.Background(), case_, owner)
	if err == nil {
		t.Fatalf("durable orchestration swallowed an owner-verification error: %+v", res)
	}
	if errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("owner-verification error was misreported as lease loss: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "lock wait timeout") {
		t.Fatalf("error = %v, want the real MySQL lock wait timeout", err)
	}
	stored, getErr := repo.CaseRepo().Get(context.Background(), case_.ID)
	if getErr != nil {
		t.Fatalf("reload case: %v", getErr)
	}
	if t3IsSuccessTerminal(stored.Status) {
		t.Fatalf("case reached success terminal %s after owner verification failed", stored.Status)
	}
	if res != nil {
		t.Fatalf("failed execution returned a resolution: %+v", res)
	}
}

// P1, unknown-outcome boundary at the orchestration level: when an authoritative
// write is abandoned while the server statement is still running, the client
// cannot know whether it landed. The durable execution must not report a
// successful decision; recovery belongs to the next generation.
//
// Not covered, by construction: a literal post-COMMIT reply loss. GORM and
// go-sql-driver expose no seam to drop only the COMMIT acknowledgement, so this
// asserts the aborted-in-flight variant of the same "unknown outcome" class.
func TestArtifactGeneration_DurableOrchestrationAbandonsUnknownOutcome(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	case_, owner := t3DurableCase(t, db, repo, jobs, job, "worker-orch-unknown-"+uuid.NewString())
	reg := metrics.New()

	trigger := "t3_orch_slow_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	ddl := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON magi_agent_run
		FOR EACH ROW
		BEGIN
			DO SLEEP(2);
		END`, trigger)
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create slow-insert trigger: %v", err)
	}
	defer func() { _ = db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error }()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	res, err := t3Orchestrator(t, repo, reg).OrchestrateForExecution(ctx, case_, owner)
	if err == nil {
		t.Fatalf("abandoned authoritative write produced a successful decision: %+v", res)
	}
	stored, getErr := repo.CaseRepo().Get(context.Background(), case_.ID)
	if getErr != nil {
		t.Fatalf("reload case: %v", getErr)
	}
	if t3IsSuccessTerminal(stored.Status) {
		t.Fatalf("case reached success terminal %s after an abandoned write", stored.Status)
	}
	if res != nil {
		t.Fatalf("abandoned execution returned a resolution: %+v", res)
	}
}
