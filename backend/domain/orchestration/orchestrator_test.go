package orchestration_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
	"github.com/jamespud/magi/backend/server"
)

// --- mocks ---

type scriptedChatModel struct {
	mu        sync.Mutex
	responses []*schema.Message
	calls     int
}

func (s *scriptedChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.responses) {
		return nil, fmt.Errorf("no more responses")
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}
func (s *scriptedChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *scriptedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

type stubModelPort struct{ m model.ToolCallingChatModel }

func (s *stubModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return s.m, nil
}

type mockMagiRuntime struct {
	mu         sync.Mutex
	votes      map[string][]*entity.Vote // code -> [vote per round]
	calls      map[string]int
	errOn      map[string]bool    // code -> return error
	failFirst  map[string]bool    // code -> fail only on the first run (retry test)
	loopStatus runtime.LoopStatus // status to return (default Completed); 0 means Completed
}

func newMockMagiRuntime() *mockMagiRuntime {
	return &mockMagiRuntime{votes: make(map[string][]*entity.Vote), calls: make(map[string]int), errOn: make(map[string]bool), failFirst: make(map[string]bool)}
}

func (m *mockMagiRuntime) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	m.mu.Lock()
	code := cfg.Code
	round := m.calls[code]
	m.calls[code]++
	m.mu.Unlock()

	if m.errOn[code] || (m.failFirst[code] && round == 0) {
		return &runtime.LoopResult{Status: runtime.LoopStatusError, Err: fmt.Errorf("mock error for %s", code)}, fmt.Errorf("mock error")
	}

	votes := m.votes[code]
	var vote *entity.Vote
	if round < len(votes) {
		vote = votes[round]
	} else if len(votes) > 0 {
		vote = votes[len(votes)-1]
	}
	if vote == nil {
		vote = &entity.Vote{Decision: entity.VoteDecisionApprove, Confidence: 90}
	}

	ledger := evidence.NewEvidenceLedger(actx.CaseID, "", code)
	ledger.Record("tc", "tool", "local", "", "observation", entity.ReliabilityScore{Final: 0.9})
	ledger.RecordClaim("claim by "+code, nil, nil) // each agent produces CL-001 -- collides without namespacing

	status := m.loopStatus
	if status == "" {
		status = runtime.LoopStatusCompleted
	}
	return &runtime.LoopResult{
		Vote:   vote,
		Status: status,
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

// --- helpers ---

func magiCfg(code string) *entity.MagiConfig {
	return &entity.MagiConfig{
		Code: code, Persona: code,
		Objective:    entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{{Code: "correctness", Weight: 0.5, Description: "be correct"}}},
		RiskTendency: entity.RiskTendencyNeutral,
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount: 1, MinQuantitativeCount: 1, MinReliability: 0,
			RequireOwnCollected: true, RequiredTypes: []entity.EvidenceTypeRequirement{{Type: "quantitative", MinCount: 1}},
		},
		Model:      entity.ModelRef{ModelID: 1},
		Tools:      []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
		LoopPolicy: entity.LoopPolicy{MaxSteps: 12},
	}
}

func newCommander(t *testing.T) *service.Commander {
	t.Helper()
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	cm := &scriptedChatModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"canonical_question":"compute"}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"decision report summary","key_reasons":["r1"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd, err := service.NewCommander(service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"}, &stubModelPort{m: cm}, gen, val)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}
	return cmd
}

func approve() *entity.Vote {
	return &entity.Vote{Decision: entity.VoteDecisionApprove, Confidence: 90, EvidenceIDs: []string{"EV-001"}}
}
func reject() *entity.Vote {
	return &entity.Vote{Decision: entity.VoteDecisionReject, Confidence: 70, EvidenceIDs: []string{"EV-001"}}
}
func conditionalApprove() *entity.Vote {
	return &entity.Vote{
		Decision:   entity.VoteDecisionConditionalApprove,
		Confidence: 80,
		Conditions: []entity.DecisionCondition{{Statement: "must have 2+ Rust engineers", MustHold: true}},
	}
}

// --- tests ---

func TestOrchestrate_UnanimousApprove(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusStrongApproval {
		t.Fatalf("resolution: %+v", res)
	}
	if res.Evaluation == nil {
		t.Fatalf("resolution should carry Evaluation (Evaluate result must not be discarded)")
	}
	if !strings.Contains(res.FinalReport, "decision report summary") {
		t.Fatalf("report: %s", res.FinalReport)
	}
}

func TestOrchestrate_ConflictThenReconsider(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve(), approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{reject(), approve()} // reject round 1, approve round 2

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 2})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusStrongApproval {
		t.Fatalf("expected strong approval after reconsider, got: %+v", res)
	}
}

func TestOrchestrate_Deadlock(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{reject()}
	mrt.votes["casper"] = []*entity.Vote{{Decision: entity.VoteDecisionAbstain, Confidence: 0}}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	c := &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1}
	res, err := orch.Orchestrate(context.Background(), c)
	if err != nil {
		t.Fatalf("deadlock is a terminal outcome, not a retryable error: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil resolution on deadlock")
	}
	if c.Status != entity.CaseStatusDeadlocked {
		t.Fatalf("expected case status DEADLOCKED, got %s", c.Status)
	}
}

// TestOrchestrate_FailureCausedTieFailsCase guards deadlock semantics: when a
// 1:1:1 tie is produced by an agent FAILURE (ABSTAIN from failure policy)
// rather than by a genuine abstention, the case must end FAILED with the
// failure reason visible instead of a misleading DEADLOCKED.
func TestOrchestrate_FailureCausedTieFailsCase(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.errOn["melchior"] = true // melchior fails -> ABSTAIN via failure policy
	mrt.votes["balthasar"] = []*entity.Vote{reject()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	c := &entity.DecisionCase{ID: "c-fail-tie", Question: "compute", MaxDebateRounds: 1}
	_, err := orch.Orchestrate(context.Background(), c)
	if err == nil {
		t.Fatal("expected case failure when a tie is caused by agent failure")
	}
	if c.Status != entity.CaseStatusFailed {
		t.Fatalf("expected case status FAILED, got %s", c.Status)
	}
	if !strings.Contains(err.Error(), "agent failed") && !strings.Contains(err.Error(), "failed") {
		t.Fatalf("failure reason not surfaced: %v", err)
	}
}

// TestOrchestrate_RetriesFailedAgent guards the retry path: an agent that
// fails on its first attempt is re-dispatched (bounded) instead of being
// immediately converted to an ABSTAIN, so transient failures do not
// fabricate ties.
func TestOrchestrate_RetriesFailedAgent(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.failFirst["melchior"] = true // first attempt fails, retry succeeds
	mrt.votes["melchior"] = []*entity.Vote{reject(), reject(), reject()}
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{approve(), approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
		FailPolicy: orchestration.FailurePolicy{Mode: "abstain_on_fail", RetryLimit: 1},
	})

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c-retry", Question: "compute", MaxDebateRounds: 2})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil {
		t.Fatal("expected resolution after agent retry")
	}
	if mrt.calls["melchior"] < 2 {
		t.Fatalf("expected melchior to be re-dispatched, calls=%d", mrt.calls["melchior"])
	}
}

// TestOrchestrate_RetryExhaustedFallsBack guards the bounded-retry bound:
// when the agent keeps failing, the failure policy still applies after the
// retry budget is consumed.
func TestOrchestrate_RetryExhaustedFallsBack(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.errOn["melchior"] = true // always fails
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{approve(), approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
		FailPolicy: orchestration.FailurePolicy{Mode: "abstain_on_fail", RetryLimit: 2},
	})

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c-retry-exhausted", Question: "compute", MaxDebateRounds: 2})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil {
		t.Fatal("expected resolution via majority of the two healthy agents")
	}
	// investigate (1 initial + 2 retries) + reconsider (same) = 6; the retry
	// budget is bounded per dispatch round, so the persistent failure still
	// falls back to the failure policy instead of looping forever.
	if mrt.calls["melchior"] != 6 {
		t.Fatalf("expected 6 melchior attempts (2 rounds x 3), got %d", mrt.calls["melchior"])
	}
}

func TestOrchestrate_FailurePolicy(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.errOn["melchior"] = true // melchior fails
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{approve(), approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 2})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil {
		t.Fatalf("expected resolution despite failure")
	}
}

func TestOrchestrate_FirstRoundSplitMaxDebateOne(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve(), approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{reject(), approve()}

	policy := consensus.DefaultConsensusPolicy()
	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    policy,
	})
	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{
		ID: "c1", Question: "q", MaxDebateRounds: 1,
	})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusStrongApproval {
		t.Fatalf("expected strong approval after debate+revote, got: %+v", res)
	}
	if res.Consensus.Round != 2 {
		t.Fatalf("expected round 2 (debate+revote), got %d", res.Consensus.Round)
	}
}

func TestEnforceReflectionRule_RevertsUnjustifiedChange(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c", "r", "m")
	ev := ledger.Record("tc", "tool", "local", "", "obs", entity.ReliabilityScore{Final: 0.9})
	prev := []*entity.Vote{{Decision: entity.VoteDecisionReject, EvidenceIDs: []string{ev.ID}}}
	newVotes := []*entity.Vote{{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{ev.ID}}}
	results := []*runtime.LoopResult{{Ledger: ledger}}
	configs := []*entity.MagiConfig{{ReflectionPolicy: entity.ReflectionPolicy{RequireJustification: true}}}
	orchestration.EnforceReflectionRule(prev, newVotes, results, configs, 1)
	if newVotes[0].Decision != entity.VoteDecisionReject {
		t.Fatalf("unjustified change should revert to reject, got %s", newVotes[0].Decision)
	}
}

func TestEnforceReflectionRule_AllowsJustifiedChange(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c", "r", "m")
	ev1 := ledger.Record("tc1", "tool", "local", "", "obs1", entity.ReliabilityScore{Final: 0.9})
	ev2 := ledger.Record("tc2", "tool", "local", "", "obs2", entity.ReliabilityScore{Final: 0.9})
	prev := []*entity.Vote{{Decision: entity.VoteDecisionReject, EvidenceIDs: []string{ev1.ID}}}
	newVotes := []*entity.Vote{{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{ev1.ID, ev2.ID}}}
	results := []*runtime.LoopResult{{Ledger: ledger}}
	configs := []*entity.MagiConfig{{ReflectionPolicy: entity.ReflectionPolicy{RequireJustification: true}}}
	orchestration.EnforceReflectionRule(prev, newVotes, results, configs, 1)
	if newVotes[0].Decision != entity.VoteDecisionApprove {
		t.Fatalf("justified change should be kept, got %s", newVotes[0].Decision)
	}
}

func TestEnforceReflectionRule_DisabledNoRevert(t *testing.T) {
	prev := []*entity.Vote{{Decision: entity.VoteDecisionReject}}
	newVotes := []*entity.Vote{{Decision: entity.VoteDecisionApprove}}
	configs := []*entity.MagiConfig{{ReflectionPolicy: entity.ReflectionPolicy{RequireJustification: false}}}
	orchestration.EnforceReflectionRule(prev, newVotes, nil, configs, 1)
	if newVotes[0].Decision != entity.VoteDecisionApprove {
		t.Fatalf("disabled policy should not revert, got %s", newVotes[0].Decision)
	}
}

func TestOrchestrate_ConditionalConsensusResolves(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{conditionalApprove()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})
	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "q", MaxDebateRounds: 1})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusConditional {
		t.Fatalf("expected ConsensusConditional, got: %+v", res)
	}
	if res.FinalDecision != entity.VoteDecisionConditionalApprove {
		t.Fatalf("expected final decision conditional_approve, got %s", res.FinalDecision)
	}
	if len(res.Consensus.Conditions) != 1 {
		t.Fatalf("expected 1 condition carried to resolution, got %d", len(res.Consensus.Conditions))
	}
}

func TestOrchestrate_StoresProjection(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}
	kp := &mockKnowledgePort{}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
		Knowledge: kp,
	})
	_, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "q", MaxDebateRounds: 1})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(kp.stored) != 1 {
		t.Fatalf("expected 1 projection stored, got %d", len(kp.stored))
	}
	if kp.stored[0] == nil {
		t.Fatal("stored projection is nil")
	}
}

func TestEnforceReflectionRule_LLMReflectionUnjustifiedReverts(t *testing.T) {
	prev := []*entity.Vote{{Decision: entity.VoteDecisionReject}}
	newVotes := []*entity.Vote{{Decision: entity.VoteDecisionApprove}}
	results := []*runtime.LoopResult{{Reflection: &entity.Reflection{PositionChange: entity.PositionChangeChange}}}
	configs := []*entity.MagiConfig{{ReflectionPolicy: entity.ReflectionPolicy{RequireJustification: true}}}
	orchestration.EnforceReflectionRule(prev, newVotes, results, configs, 1)
	if newVotes[0].Decision != entity.VoteDecisionReject {
		t.Fatalf("unjustified LLM reflection should revert to reject, got %s", newVotes[0].Decision)
	}
}

func TestEnforceReflectionRule_LLMReflectionJustifiedKept(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c", "r", "m")
	ev := ledger.Record("tc", "tool", "local", "", "obs", entity.ReliabilityScore{Final: 0.9})
	prev := []*entity.Vote{{Decision: entity.VoteDecisionReject, EvidenceIDs: []string{ev.ID}}}
	newVotes := []*entity.Vote{{Decision: entity.VoteDecisionApprove, EvidenceIDs: []string{ev.ID}}}
	results := []*runtime.LoopResult{{Ledger: ledger, Reflection: &entity.Reflection{PositionChange: entity.PositionChangeChange, NewEvidenceIDs: []string{ev.ID}}}}
	configs := []*entity.MagiConfig{{ReflectionPolicy: entity.ReflectionPolicy{RequireJustification: true}}}
	orchestration.EnforceReflectionRule(prev, newVotes, results, configs, 1)
	if newVotes[0].Decision != entity.VoteDecisionApprove {
		t.Fatalf("justified LLM reflection should be kept, got %s", newVotes[0].Decision)
	}
}

// --- stub aggregate repository (records Creates) ---

type stubRepo struct {
	mu          sync.Mutex
	cases       []*entity.DecisionCase
	statuses    map[string]entity.CaseStatus
	agentRuns   []*entity.AgentRun
	evidence    []*entity.EvidenceRecord
	claims      []*entity.Claim
	votes       []*entity.Vote
	resolutions []*entity.Resolution
	toolCalls   []*entity.ToolCall
}

func newStubRepo() *stubRepo { return &stubRepo{statuses: map[string]entity.CaseStatus{}} }

func (s *stubRepo) CaseRepo() port.CaseRepository             { return &stubCaseRepo{s: s} }
func (s *stubRepo) AgentRunRepo() port.AgentRunRepository     { return &stubAgentRunRepo{s: s} }
func (s *stubRepo) EvidenceRepo() port.EvidenceRepository     { return &stubEvidenceRepo{s: s} }
func (s *stubRepo) ClaimRepo() port.ClaimRepository           { return &stubClaimRepo{s: s} }
func (s *stubRepo) VoteRepo() port.VoteRepository             { return &stubVoteRepo{s: s} }
func (s *stubRepo) DebateRepo() port.DebateRepository         { return &stubDebateRepo{} }
func (s *stubRepo) ReflectionRepo() port.ReflectionRepository { return &stubReflRepo{} }
func (s *stubRepo) ResolutionRepo() port.ResolutionRepository { return &stubResRepo{s: s} }
func (s *stubRepo) EventRepo() port.EventRepository           { return &stubEventRepo{} }
func (s *stubRepo) CheckpointRepo() port.CheckpointRepository { return &stubCpRepo{} }
func (s *stubRepo) MemoryRepo() port.MemoryRepository         { return &stubMemRepo{} }
func (s *stubRepo) ToolCallRepo() port.ToolCallRepository     { return &stubToolCallRepo{s: s} }

type stubCaseRepo struct{ s *stubRepo }

func (r *stubCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.cases = append(r.s.cases, c)
	return nil
}
func (r *stubCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (r *stubCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (r *stubCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.statuses[id] = st
	return nil
}
func (r *stubCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

type stubAgentRunRepo struct{ s *stubRepo }

func (r *stubAgentRunRepo) Create(ctx context.Context, a *entity.AgentRun) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.agentRuns = append(r.s.agentRuns, a)
	return nil
}
func (r *stubAgentRunRepo) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	return nil, nil
}
func (r *stubAgentRunRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	return nil, nil
}
func (r *stubAgentRunRepo) SumUsageByUser(ctx context.Context, userID int64) (int64, float64, error) {
	return 0, 0, nil
}

type stubEvidenceRepo struct{ s *stubRepo }

func (r *stubEvidenceRepo) Create(ctx context.Context, e *entity.EvidenceRecord) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.evidence = append(r.s.evidence, e)
	return nil
}
func (r *stubEvidenceRepo) Get(ctx context.Context, id string) (*entity.EvidenceRecord, error) {
	return nil, nil
}
func (r *stubEvidenceRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error) {
	return nil, nil
}

type stubClaimRepo struct{ s *stubRepo }

func (r *stubClaimRepo) Create(ctx context.Context, c *entity.Claim) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.claims = append(r.s.claims, c)
	return nil
}
func (r *stubClaimRepo) Get(ctx context.Context, id string) (*entity.Claim, error) { return nil, nil }
func (r *stubClaimRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Claim, error) {
	return nil, nil
}

type stubVoteRepo struct{ s *stubRepo }

func (r *stubVoteRepo) Create(ctx context.Context, v *entity.Vote) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.votes = append(r.s.votes, v)
	return nil
}
func (r *stubVoteRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Vote, error) {
	return nil, nil
}

type stubDebateRepo struct{}

func (stubDebateRepo) Create(ctx context.Context, d *entity.DebateRound) error { return nil }
func (stubDebateRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.DebateRound, error) {
	return nil, nil
}

type stubReflRepo struct{}

func (stubReflRepo) Create(ctx context.Context, r *entity.Reflection) error { return nil }
func (stubReflRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Reflection, error) {
	return nil, nil
}

type stubToolCallRepo struct{ s *stubRepo }

func (r *stubToolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *t
	r.s.toolCalls = append(r.s.toolCalls, &cp)
	return nil
}
func (r *stubToolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	return nil, nil
}

type stubResRepo struct{ s *stubRepo }

func (r *stubResRepo) Create(ctx context.Context, res *entity.Resolution) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	// Snapshot like a real DB INSERT: later mutations of the in-memory resolution
	// (e.g. FinalReport set in GeneratingReport) must NOT be visible in the
	// persisted record. This catches bugs where persistence happens too early.
	cp := *res
	r.s.resolutions = append(r.s.resolutions, &cp)
	return nil
}
func (r *stubResRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	return nil, nil
}

type stubEventRepo struct{}

func (stubEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error { return nil }
func (stubEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return nil, nil
}

type stubCpRepo struct{}

func (stubCpRepo) Save(context.Context, *entity.AgentState) error           { return nil }
func (stubCpRepo) Load(context.Context, string) (*entity.AgentState, error) { return nil, nil }

type stubMemRepo struct{}

func (stubMemRepo) Get(context.Context, string) (*entity.CaseMemoryProjection, error) {
	return nil, nil
}
func (stubMemRepo) Save(context.Context, *entity.CaseMemoryProjection) error { return nil }
func (stubMemRepo) Search(context.Context, string, int) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}

func TestOrchestrate_PersistsArtifacts(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}
	repo := newStubRepo()

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	_, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.evidence) == 0 {
		t.Fatal("no evidence persisted")
	}
	if len(repo.votes) != 3 {
		t.Fatalf("expected 3 votes persisted, got %d", len(repo.votes))
	}
	// Each vote must link to its agent run via AgentRunID so /agents can join.
	runIDs := map[string]bool{}
	for _, r := range repo.agentRuns {
		runIDs[r.ID] = true
	}
	for _, v := range repo.votes {
		if v.AgentRunID == "" || !runIDs[v.AgentRunID] {
			t.Fatalf("vote %s AgentRunID %q does not match any agent run", v.ID, v.AgentRunID)
		}
	}
	if len(repo.agentRuns) != 3 {
		t.Fatalf("expected 3 agent runs persisted, got %d", len(repo.agentRuns))
	}
	if len(repo.resolutions) != 1 {
		t.Fatalf("expected 1 resolution persisted, got %d", len(repo.resolutions))
	}
	if repo.resolutions[0].FinalReport == "" {
		t.Fatal("persisted resolution must have a non-empty FinalReport (persist after report generation, not before)")
	}
	if repo.statuses["c1"] != entity.CaseStatusResolved {
		t.Fatalf("expected case status RESOLVED, got %s", repo.statuses["c1"])
	}
	// Each agent produced a claim with the same in-memory ID (CL-001); persisted
	// IDs must be namespaced so they don't collide on the claim table's PK.
	if len(repo.claims) != 3 {
		t.Fatalf("expected 3 claims persisted, got %d", len(repo.claims))
	}
	seenClaims := map[string]bool{}
	for _, cl := range repo.claims {
		if seenClaims[cl.ID] {
			t.Fatalf("duplicate persisted claim ID: %s", cl.ID)
		}
		seenClaims[cl.ID] = true
		// ID must include the case ID so claims across different cases don't
		// collide on the shared claim table's primary key.
		if !strings.Contains(cl.ID, "c1-") {
			t.Fatalf("persisted claim ID must include case ID, got %q", cl.ID)
		}
	}
	seenEvidence := map[string]bool{}
	for _, ev := range repo.evidence {
		if seenEvidence[ev.ID] {
			t.Fatalf("duplicate persisted evidence ID: %s", ev.ID)
		}
		seenEvidence[ev.ID] = true
	}
	// Each agent's trace has one tool call; 3 agents -> 3 tool-call records,
	// with namespaced PKs and the agent_run_id linked.
	if len(repo.toolCalls) != 3 {
		t.Fatalf("expected 3 tool calls persisted (1 per agent), got %d", len(repo.toolCalls))
	}
	seenTC := map[string]bool{}
	for _, tc := range repo.toolCalls {
		if seenTC[tc.ID] {
			t.Fatalf("duplicate persisted tool call ID: %s", tc.ID)
		}
		seenTC[tc.ID] = true
		if tc.AgentRunID == "" {
			t.Fatalf("tool call %s missing AgentRunID", tc.ID)
		}
		if tc.ToolName != "calc" {
			t.Fatalf("tool call ToolName: %s", tc.ToolName)
		}
	}
}

// --- end-to-end async integration ---

func TestIntegration_AsyncRunPersistsAndStreamsEvents(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}
	repo := newStubRepo()
	broker := server.NewEventBroker()

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		EventPub:  broker,
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	rm := decision.NewRunManager(orch)
	case_ := &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1, Status: entity.CaseStatusDraft}

	if err := rm.Start(context.Background(), case_); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !rm.IsRunning("c1") {
		t.Fatal("should be running immediately after start")
	}

	// Wait for the async run to complete (bounded).
	deadline := time.Now().Add(5 * time.Second)
	for rm.IsRunning("c1") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rm.IsRunning("c1") {
		t.Fatal("run did not complete within 5s")
	}

	// Assert events were published with non-empty IDs.
	events, err := broker.ListByCase(context.Background(), "c1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events published")
	}
	var sawCompleted bool
	for _, ev := range events {
		if ev.ID == "" {
			t.Fatalf("event %s has empty ID", ev.Type)
		}
		if ev.Type == entity.EventCaseCompleted {
			sawCompleted = true
		}
	}
	if !sawCompleted {
		t.Fatal("no CASE_COMPLETED event")
	}

	// Assert artifacts were persisted.
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.votes) != 3 {
		t.Fatalf("expected 3 votes, got %d", len(repo.votes))
	}
	if len(repo.evidence) == 0 {
		t.Fatal("no evidence persisted")
	}
	if len(repo.resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(repo.resolutions))
	}
	if repo.statuses["c1"] != entity.CaseStatusResolved {
		t.Fatalf("case status: %s", repo.statuses["c1"])
	}
}

func TestOrchestrate_DebatePersistsUniqueClaimIDs(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve(), approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve(), approve()}
	mrt.votes["casper"] = []*entity.Vote{reject(), approve()} // round 1 split -> debate -> round 2 approve
	repo := newStubRepo()

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	_, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 2})
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}

	// Both the investigate dispatch and the reconsider dispatch happen in
	// round 1; each agent's ledger generates CL-001 in both. Persisted IDs
	// must distinguish the two phases or the reconsider claims collide.
	repo.mu.Lock()
	defer repo.mu.Unlock()
	seen := map[string]bool{}
	for _, cl := range repo.claims {
		if seen[cl.ID] {
			t.Fatalf("duplicate persisted claim ID across investigate/reconsider: %s", cl.ID)
		}
		seen[cl.ID] = true
	}
	// 3 agents x 1 claim x 2 phases (investigate + reconsider) = 6 distinct claims.
	if len(repo.claims) != 6 {
		t.Fatalf("expected 6 persisted claims (3 agents x 2 phases), got %d: %v", len(repo.claims), claimIDs(repo.claims))
	}
}

func claimIDs(cls []*entity.Claim) []string {
	out := make([]string, len(cls))
	for i, c := range cls {
		out[i] = c.ID
	}
	return out
}

func TestOrchestrate_FailureStatusMappedToFailed(t *testing.T) {
	mrt := newMockMagiRuntime()
	mrt.loopStatus = runtime.LoopStatusTokenBudget // agent hit token budget (a failure)
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}
	repo := newStubRepo()

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	_, _ = orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1})

	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, r := range repo.agentRuns {
		// Token-budget termination must NOT show as "completed"; it is a failure.
		if r.Status == entity.AgentRunStatusCompleted {
			t.Fatalf("agent run %s status should not be completed for a token-budget termination, got %s", r.ID, r.Status)
		}
	}
}
