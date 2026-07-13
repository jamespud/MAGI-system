package orchestration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
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
func (s *scriptedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) { return s, nil }

type stubModelPort struct{ m model.ToolCallingChatModel }

func (s *stubModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return s.m, nil
}

type mockMagiRuntime struct {
	mu    sync.Mutex
	votes map[string][]*entity.Vote // code -> [vote per round]
	calls map[string]int
	errOn map[string]bool // code -> return error
}

func newMockMagiRuntime() *mockMagiRuntime {
	return &mockMagiRuntime{votes: make(map[string][]*entity.Vote), calls: make(map[string]int), errOn: make(map[string]bool)}
}

func (m *mockMagiRuntime) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	m.mu.Lock()
	code := cfg.Code
	round := m.calls[code]
	m.calls[code]++
	m.mu.Unlock()

	if m.errOn[code] {
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

	return &runtime.LoopResult{
		Vote:   vote,
		Status: runtime.LoopStatusCompleted,
		Ledger: ledger,
		Trace:  &runtime.LoopTrace{Steps: []*runtime.Step{{IsFinal: true}}},
		Usage:  &entity.Usage{TotalTokens: 100},
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
		schema.AssistantMessage("decision report", nil),
	}}
	cmd, err := service.NewCommander(service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"}, &stubModelPort{m: cm}, gen, val)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}
	return cmd
}

func approve() *entity.Vote { return &entity.Vote{Decision: entity.VoteDecisionApprove, Confidence: 90, EvidenceIDs: []string{"EV-001"}} }
func reject() *entity.Vote  { return &entity.Vote{Decision: entity.VoteDecisionReject, Confidence: 70, EvidenceIDs: []string{"EV-001"}} }

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
	if res.FinalReport != "decision report" {
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

	res, err := orch.Orchestrate(context.Background(), &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1})
	if err == nil {
		t.Fatalf("expected error for deadlock")
	}
	if res != nil {
		t.Fatalf("expected nil resolution on deadlock")
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
