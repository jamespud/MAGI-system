package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

// --- stubs ---

type scriptedChatModel struct {
	responses []*schema.Message
	calls     int
}

func (s *scriptedChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if s.calls >= len(s.responses) {
		return nil, fmt.Errorf("scripted model: no more responses")
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}
func (s *scriptedChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}
func (s *scriptedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) { return s, nil }

type stubModelPort struct{ m model.ToolCallingChatModel }

func (s *stubModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return s.m, nil
}

type stubToolReg struct{ defs []port.ToolDefinition }

func (s *stubToolReg) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	return s.defs, nil
}

type calcArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

type stubToolExec struct{}

func (s *stubToolExec) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var a calcArgs
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &a); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &port.ToolExecutionResult{Output: fmt.Sprintf("%d", a.A+a.B)}, nil
}

// --- helpers ---

func callMsg(id, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
func finalMsg(content string) *schema.Message { return schema.AssistantMessage(content, nil) }
func withUsage(m *schema.Message, p, c, t int) *schema.Message {
	m.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: p, CompletionTokens: c, TotalTokens: t}}
	return m
}

func evidenceCfg(minQ, minRel float64) *entity.MagiConfig {
	return &entity.MagiConfig{
		Code: "melchior", Persona: "scientist",
		Objective:    entity.ObjectiveFunction{Dimensions: []entity.UtilityDimension{{Code: "correctness", Weight: 0.5, Description: "be correct"}}},
		RiskTendency: entity.RiskTendencyNeutral,
		EvidenceStandard: entity.EvidenceStandard{
			MinEvidenceCount: 1, MinQuantitativeCount: int(minQ), MinReliability: minRel,
			RequireOwnCollected: true, RequiredTypes: []entity.EvidenceTypeRequirement{{Type: "quantitative", MinCount: 1}},
		},
		Model: entity.ModelRef{ModelID: 1},
		Tools: []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
		LoopPolicy: entity.LoopPolicy{MaxSteps: 12},
	}
}

func newAgentLoop(t *testing.T, responses []*schema.Message, bindingRel *float64) *runtime.AgentLoop {
	t.Helper()
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc", Reliability: bindingRel}
	deps := runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: responses}},
		ToolReg:   &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding}}},
		ToolExec:  &stubToolExec{},
		Validator: v, Gen: gen,
	}
	loop, err := runtime.NewAgentLoop(deps)
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}
	return loop
}

func summaryJSON(ids ...string) string {
	q := make([]string, len(ids))
	for i, id := range ids { q[i] = `"` + id + `"` }
	return fmt.Sprintf(`{"evidence_by_type":{"quantitative":[%s]},"claims":[],"ready":true}`, strings.Join(q, ","))
}
func summaryJSONWithClaim(supports string) string {
	return fmt.Sprintf(`{"evidence_by_type":{"quantitative":["EV-001"]},"claims":[{"statement":"s","supports":[%s],"contradicts":[]}],"ready":true}`, supports)
}
func voteJSON(dim string) string {
	return fmt.Sprintf(`{"decision":"approve","confidence":80,"utility_scores":[{"dimension_code":%q,"score":90,"evidence_ids":["EV-001"],"reasoning":"r"}],"evidence_ids":["EV-001"]}`, dim)
}

// --- tests ---

func TestAgentLoop_FullFlow(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if res.Vote == nil || res.Vote.Decision != entity.VoteDecisionApprove { t.Fatalf("vote: %+v", res.Vote) }
	if ev, ok := res.Ledger.Get("EV-001"); !ok || ev.Observation != "3" { t.Fatalf("evidence: %+v", ev) }
	if len(res.Trace.Steps) != 3 || !res.Trace.Steps[2].IsFinal { t.Fatalf("trace: %d", len(res.Trace.Steps)) }
}

func TestAgentLoop_GateFailSelfHeal(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		callMsg("c2", "calc", `{"a":2,"b":3}`),
		finalMsg(summaryJSON("EV-001", "EV-002")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(2, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil { t.Fatalf("run: %v", err) }
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if len(res.Trace.Steps) != 5 { t.Fatalf("steps: %d", len(res.Trace.Steps)) }
}

func TestAgentLoop_SummaryInvalidSelfHeal(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg("not json"),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil { t.Fatalf("run: %v", err) }
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if len(res.Trace.Steps) != 4 { t.Fatalf("steps: %d", len(res.Trace.Steps)) }
}

func TestAgentLoop_VoteDimensionInvalid(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("bogus")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil { t.Fatalf("run: %v", err) }
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if len(res.Trace.Steps) != 4 { t.Fatalf("steps: %d", len(res.Trace.Steps)) }
}

func TestAgentLoop_ClaimUnsupported(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSONWithClaim(`"EV-999"`)),
		finalMsg(summaryJSONWithClaim(`"EV-001"`)),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil { t.Fatalf("run: %v", err) }
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if len(res.Trace.Steps) != 4 { t.Fatalf("steps: %d", len(res.Trace.Steps)) }
}

func TestAgentLoop_ReliabilityFromBinding(t *testing.T) {
	rel := 0.95
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, &rel)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0.7), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil { t.Fatalf("run: %v", err) }
	ev, ok := res.Ledger.Get("EV-001")
	if !ok || ev.Reliability.Base != 0.95 { t.Fatalf("reliability base should reflect binding override: %+v", ev) }
	if ev.Reliability.Final == ev.Reliability.Base { t.Fatalf("Final should be a weighted average, not == Base: %+v", ev) }
}

func TestAgentLoop_MaxSteps(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(summaryJSON("EV-001")),
	}, nil)
	cfg := evidenceCfg(2, 0)
	cfg.LoopPolicy.MaxSteps = 3
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if !errors.Is(err, runtime.ErrMaxSteps) { t.Fatalf("err: %v", err) }
	if res.Status != runtime.LoopStatusMaxSteps { t.Fatalf("status: %v", res.Status) }
}

func TestAgentLoop_Cancellation(t *testing.T) {
	loop := newAgentLoop(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := loop.Run(ctx, evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err == nil { t.Fatalf("expected error") }
	if res.Status != runtime.LoopStatusCancelled { t.Fatalf("status: %v", res.Status) }
}

func TestAgentLoop_Reconsider(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	prevVote := &entity.Vote{Decision: entity.VoteDecisionReject, Confidence: 60}
	actx := &runtime.AgentContext{
		Task:          entity.DecisionTask{CanonicalQuestion: "reconsider"},
		DebateContext: &runtime.DebateContext{PreviousVote: prevVote},
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), actx)
	if err != nil { t.Fatalf("run: %v", err) }
	if res.Status != runtime.LoopStatusCompleted { t.Fatalf("status: %v", res.Status) }
	if res.Vote == nil { t.Fatalf("no vote") }
}

func TestAgentLoop_TokenBudget(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		withUsage(callMsg("c1", "calc", `{"a":1,"b":2}`), 10, 5, 15),
	}, nil)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.TokenBudget = 1
	res, _ := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if res.Status != runtime.LoopStatusTokenBudget { t.Fatalf("status: %v", res.Status) }
}

func TestAgentLoop_GateFailLimit(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
	}, nil)
	cfg := evidenceCfg(2, 0)
	cfg.LoopPolicy.MaxGateFailures = 1
	res, _ := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if res.Status != runtime.LoopStatusGateFailed { t.Fatalf("status: %v", res.Status) }
}

func TestRelaxEvidenceStandard_PreservesCustomRules(t *testing.T) {
	std := entity.EvidenceStandard{
		MinEvidenceCount:   3,
		RequiredClaimCount: 2,
		CustomRules:        []entity.EvidenceRule{{Code: "worst_case_claim_required"}},
	}
	relaxed := runtime.RelaxEvidenceStandard(std, false)
	if relaxed.MinEvidenceCount != 0 {
		t.Fatalf("MinEvidenceCount should be zeroed, got %d", relaxed.MinEvidenceCount)
	}
	if relaxed.RequiredClaimCount != 0 {
		t.Fatalf("RequiredClaimCount should be zeroed, got %d", relaxed.RequiredClaimCount)
	}
	if len(relaxed.CustomRules) != 1 {
		t.Fatalf("CustomRules should be preserved, got %d", len(relaxed.CustomRules))
	}
	if relaxed.CustomRules[0].Code != "worst_case_claim_required" {
		t.Fatalf("preserved rule code: %s", relaxed.CustomRules[0].Code)
	}
}

func TestRelaxEvidenceStandard_WithToolsUnchanged(t *testing.T) {
	std := entity.EvidenceStandard{
		MinEvidenceCount: 3,
		CustomRules:      []entity.EvidenceRule{{Code: "primary_source_required"}},
	}
	got := runtime.RelaxEvidenceStandard(std, true)
	if got.MinEvidenceCount != 3 {
		t.Fatalf("with tools should be unchanged, got MinEvidenceCount=%d", got.MinEvidenceCount)
	}
	if len(got.CustomRules) != 1 {
		t.Fatalf("with tools CustomRules unchanged, got %d", len(got.CustomRules))
	}
}

func TestAgentLoop_UsesFullReliabilityResolver(t *testing.T) {
	rel := 0.9
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, &rel)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	ev, ok := res.Ledger.Get("EV-001")
	if !ok || ev == nil {
		t.Fatalf("evidence EV-001 not found")
	}
	if ev.Reliability.Final == ev.Reliability.Base {
		t.Fatalf("Final==Base (%v): FullReliabilityResolver not wired; expected weighted average", ev.Reliability.Final)
	}
	if ev.Reliability.Directness == 0 || ev.Reliability.Extraction == 0 {
		t.Fatalf("modifiers should be non-zero with FullReliabilityResolver: %+v", ev.Reliability)
	}
}

type recordingEventPub struct {
	events []entity.MagiEvent
}

func (r *recordingEventPub) Publish(ctx context.Context, e entity.MagiEvent) error {
	r.events = append(r.events, e)
	return nil
}

func TestAgentLoop_GatePassSingleClaimCreatedEvent(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "calc", `{"a":1,"b":2}`),
			finalMsg(summaryJSONWithClaim(`"EV-001"`)),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg:   &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding}}},
		ToolExec:  &stubToolExec{},
		Validator: v, Gen: gen,
		EventPub:  rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	count := 0
	for _, e := range rec.events {
		if e.Type == entity.EventClaimCreated {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 EventClaimCreated (1 claim in summary), got %d", count)
	}
}

func TestAgentLoop_EventsCarryRunID(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "calc", `{"a":1,"b":2}`),
			finalMsg(summaryJSON("EV-001")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg:   &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding}}},
		ToolExec:  &stubToolExec{},
		Validator: v, Gen: gen,
		EventPub:  rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	actx := &runtime.AgentContext{CaseID: "c1", RunID: "c1-melchior-r1-investigate", Task: entity.DecisionTask{CanonicalQuestion: "compute"}}
	_, err = loop.Run(context.Background(), evidenceCfg(1, 0), actx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.events) == 0 {
		t.Fatal("expected events to be published")
	}
	for _, e := range rec.events {
		if e.RunID != "c1-melchior-r1-investigate" {
			t.Fatalf("event RunID=%q want=%q (type=%s)", e.RunID, "c1-melchior-r1-investigate", e.Type)
		}
	}
}
