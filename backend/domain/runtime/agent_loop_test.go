package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
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
func (s *scriptedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

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
		Model:      entity.ModelRef{ModelID: 1},
		Tools:      []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
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
	for i, id := range ids {
		q[i] = `"` + id + `"`
	}
	return fmt.Sprintf(`{"evidence_by_type":{"quantitative":[%s]},"claims":[],"ready":true}`, strings.Join(q, ","))
}
func summaryJSONWithClaim(supports string) string {
	return fmt.Sprintf(`{"evidence_by_type":{"quantitative":["EV-001"]},"claims":[{"statement":"s","supports":[%s],"contradicts":[]}],"ready":true}`, supports)
}
func voteJSON(dim string) string {
	return fmt.Sprintf(`{"decision":"approve","confidence":80,"utility_scores":[{"dimension_code":%q,"score":90,"evidence_ids":["EV-001"],"reasoning":"r"}],"evidence_ids":["EV-001"]}`, dim)
}
func riskSummaryJSON(residual float64) string {
	return fmt.Sprintf(`{"evidence_by_type":{"quantitative":["EV-001"]},"claims":[],"role_assessment":{"dimension_assessments":[{"dimension_code":"correctness","score":80,"evidence_ids":["EV-001"],"reasoning":"supported"}],"risk":{"worst_case":"data loss","residual_risk":%.2f,"reversibility_score":20,"rollback_plan":"restore previous deployment","risk_evidence_ids":["EV-001"]}},"ready":true}`, residual)
}
func reflectionJSON(posChange string) string {
	return fmt.Sprintf(`{"position_change":%q,"new_evidence_ids":["EV-001"],"reasoning":"changed based on debate","ready_to_revote":true}`, posChange)
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
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if res.Vote == nil || res.Vote.Decision != entity.VoteDecisionApprove {
		t.Fatalf("vote: %+v", res.Vote)
	}
	if ev, ok := res.Ledger.Get("EV-001"); !ok || ev.Observation != "3" {
		t.Fatalf("evidence: %+v", ev)
	}
	if len(res.Trace.Steps) != 3 || !res.Trace.Steps[2].IsFinal {
		t.Fatalf("trace: %d", len(res.Trace.Steps))
	}
}

func TestAgentLoop_RoleContractAndDecisionBoundary(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")), // role assessment missing
		finalMsg(riskSummaryJSON(0.80)), // role assessment passes, approval boundary does not
		finalMsg(voteJSON("correctness")),
		finalMsg(strings.Replace(voteJSON("correctness"), `"approve"`, `"reject"`, 1)),
	}, nil)
	cfg := evidenceCfg(1, 0)
	cfg.Code = "balthasar"
	cfg.RolePolicy = entity.DefaultRolePolicy("balthasar")
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "deploy"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted || res.Vote == nil || res.Vote.Decision != entity.VoteDecisionReject {
		t.Fatalf("role boundary should force a valid conservative dissent, result=%+v", res)
	}
	if len(res.Trace.Steps) != 5 {
		t.Fatalf("expected role gate failure and approval-boundary self-heal, steps=%d", len(res.Trace.Steps))
	}
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
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(res.Trace.Steps) != 5 {
		t.Fatalf("steps: %d", len(res.Trace.Steps))
	}
}

func TestAgentLoop_SummaryInvalidSelfHeal(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg("not json"),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(res.Trace.Steps) != 4 {
		t.Fatalf("steps: %d", len(res.Trace.Steps))
	}
}

func TestAgentLoop_VoteDimensionInvalid(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("bogus")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(res.Trace.Steps) != 4 {
		t.Fatalf("steps: %d", len(res.Trace.Steps))
	}
}

func TestAgentLoop_ClaimUnsupported(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSONWithClaim(`"EV-999"`)),
		finalMsg(summaryJSONWithClaim(`"EV-001"`)),
		finalMsg(voteJSON("correctness")),
	}, nil)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(res.Trace.Steps) != 4 {
		t.Fatalf("steps: %d", len(res.Trace.Steps))
	}
}

func TestAgentLoop_ReliabilityFromBinding(t *testing.T) {
	rel := 0.95
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, &rel)
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0.7), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	ev, ok := res.Ledger.Get("EV-001")
	if !ok || ev.Reliability.Base != 0.95 {
		t.Fatalf("reliability base should reflect binding override: %+v", ev)
	}
	if ev.Reliability.Final == ev.Reliability.Base {
		t.Fatalf("Final should be a weighted average, not == Base: %+v", ev)
	}
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
	if !errors.Is(err, runtime.ErrMaxSteps) {
		t.Fatalf("err: %v", err)
	}
	if res.Status != runtime.LoopStatusMaxSteps {
		t.Fatalf("status: %v", res.Status)
	}
}

func TestAgentLoop_Cancellation(t *testing.T) {
	loop := newAgentLoop(t, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := loop.Run(ctx, evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if res.Status != runtime.LoopStatusCancelled {
		t.Fatalf("status: %v", res.Status)
	}
}

func TestAgentLoop_Reconsider(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(reflectionJSON("change")),
		finalMsg(voteJSON("correctness")),
	}, nil)
	prevVote := &entity.Vote{Decision: entity.VoteDecisionReject, Confidence: 60}
	actx := &runtime.AgentContext{
		Task:          entity.DecisionTask{CanonicalQuestion: "reconsider"},
		DebateContext: &runtime.DebateContext{PreviousVote: prevVote},
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), actx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if res.Vote == nil {
		t.Fatalf("no vote")
	}
	if res.Reflection == nil {
		t.Fatalf("expected Reflection produced in reconsider")
	}
	if res.Reflection.PositionChange != entity.PositionChangeChange {
		t.Fatalf("position change: %s", res.Reflection.PositionChange)
	}
}

func TestAgentLoop_TokenBudget(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		withUsage(callMsg("c1", "calc", `{"a":1,"b":2}`), 10, 5, 15),
	}, nil)
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.TokenBudget = 1
	res, _ := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if res.Status != runtime.LoopStatusTokenBudget {
		t.Fatalf("status: %v", res.Status)
	}
}

func TestAgentLoop_GateFailLimit(t *testing.T) {
	loop := newAgentLoop(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
	}, nil)
	cfg := evidenceCfg(2, 0)
	cfg.LoopPolicy.MaxGateFailures = 1
	res, _ := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if res.Status != runtime.LoopStatusGateFailed {
		t.Fatalf("status: %v", res.Status)
	}
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
		EventPub: rec,
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
		EventPub: rec,
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

func TestAgentLoop_ToolCallRequestedAndValidatedEvents(t *testing.T) {
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
		EventPub: rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, err = loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	count := func(et entity.EventType) int {
		n := 0
		for _, e := range rec.events {
			if e.Type == et {
				n++
			}
		}
		return n
	}
	if count(entity.EventToolCallRequested) != 1 {
		t.Fatalf("expected 1 EventToolCallRequested, got %d", count(entity.EventToolCallRequested))
	}
	if count(entity.EventToolCallValidated) != 1 {
		t.Fatalf("expected 1 EventToolCallValidated, got %d", count(entity.EventToolCallValidated))
	}
}

type mockCheckpointRepo struct {
	saved []*entity.AgentState
	load  *entity.AgentState
}

func (m *mockCheckpointRepo) Save(ctx context.Context, s *entity.AgentState) error {
	m.saved = append(m.saved, s)
	return nil
}
func (m *mockCheckpointRepo) Load(ctx context.Context, runID string) (*entity.AgentState, error) {
	return m.load, nil
}

func TestAgentLoop_CheckpointSaved(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	repo := &mockCheckpointRepo{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "calc", `{"a":1,"b":2}`),
			finalMsg(summaryJSON("EV-001")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg:  &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding}}},
		ToolExec: &stubToolExec{}, Validator: v, Gen: gen,
		CheckpointRepo: repo,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	actx := &runtime.AgentContext{CaseID: "c1", RunID: "c1-melchior-r1-investigate", Task: entity.DecisionTask{CanonicalQuestion: "compute"}}
	_, err = loop.Run(context.Background(), evidenceCfg(1, 0), actx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(repo.saved) == 0 {
		t.Fatal("expected checkpoint saves")
	}
	if repo.saved[0].RunID != "c1-melchior-r1-investigate" {
		t.Fatalf("RunID: %s", repo.saved[0].RunID)
	}
	if repo.saved[0].MessagesJSON == "" {
		t.Fatal("expected non-empty MessagesJSON")
	}
}

func TestAgentLoop_ResumeFromCheckpoint(t *testing.T) {
	msgs := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("compute"),
		schema.AssistantMessage(summaryJSON("EV-001"), nil),
		schema.UserMessage("Evidence gate passed. Now output the Vote JSON."),
	}
	msgsJSON, _ := json.Marshal(msgs)
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	repo := &mockCheckpointRepo{load: &entity.AgentState{
		RunID:        "c1-melchior-r1-investigate",
		MessagesJSON: string(msgsJSON),
		StepCount:    1,
		Phase:        "vote",
	}}
	sm := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: sm}, Validator: v, Gen: gen,
		CheckpointRepo: repo,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	actx := &runtime.AgentContext{CaseID: "c1", RunID: "c1-melchior-r1-investigate", Task: entity.DecisionTask{CanonicalQuestion: "compute"}}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), actx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v (resume failed?)", res.Status)
	}
	if res.Vote == nil {
		t.Fatal("no vote")
	}
	if sm.calls != 1 {
		t.Fatalf("expected 1 model call (resumed at step 2), got %d", sm.calls)
	}
}

func TestAgentLoop_EventsCarryIDAndPayload(t *testing.T) {
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
		EventPub: rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	actx := &runtime.AgentContext{CaseID: "c1", RunID: "c1-melchior-r1-investigate", Task: entity.DecisionTask{CanonicalQuestion: "compute"}}
	if _, err := loop.Run(context.Background(), evidenceCfg(1, 0), actx); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.events) == 0 {
		t.Fatal("no events published")
	}
	var gotToolReq, gotEvidence bool
	for _, ev := range rec.events {
		if ev.ID == "" {
			t.Fatalf("event %s has empty ID", ev.Type)
		}
		if !strings.HasPrefix(ev.ID, "c1-") {
			t.Fatalf("event ID should be prefixed with caseID, got %s", ev.ID)
		}
		if ev.Type == entity.EventToolCallRequested {
			if !bytes.Contains(ev.Payload, []byte(`"tool_name"`)) || !bytes.Contains(ev.Payload, []byte(`"calc"`)) {
				t.Fatalf("TOOL_CALL_REQUESTED payload missing tool_name=calc: %s", string(ev.Payload))
			}
			gotToolReq = true
		}
		if ev.Type == entity.EventEvidenceCreated {
			if !bytes.Contains(ev.Payload, []byte(`"evidence_id"`)) {
				t.Fatalf("EVIDENCE_CREATED payload missing evidence_id: %s", string(ev.Payload))
			}
			gotEvidence = true
		}
	}
	if !gotToolReq {
		t.Fatal("no TOOL_CALL_REQUESTED event")
	}
	if !gotEvidence {
		t.Fatal("no EVIDENCE_CREATED event")
	}
}

func TestAgentLoop_WebSearchProducesMultipleEvidence(t *testing.T) {
	// Stub Tavily server returning 2 results.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(evidence.TavilyResponse{
			Results: []evidence.TavilyResult{
				{URL: "https://a.example", Content: "result A content", Score: 0.9},
				{URL: "https://b.example", Content: "result B content", Score: 0.8},
			},
		})
	}))
	defer srv.Close()

	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	toolSchema, _ := gen.FromStruct(struct {
		Query string `json:"query"`
	}{})
	toolReg := &stubToolReg{defs: []port.ToolDefinition{{
		Name: "web_search", Desc: "search", ArgsSchema: toolSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"},
	}}}
	toolExec := magi.NewTavilyToolExecutorWithURL("k", srv.URL)
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "web_search", `{"query":"rust"}`),
			finalMsg(summaryJSON("EV-001", "EV-002")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg:   toolReg,
		ToolExec:  toolExec,
		Validator: val, Gen: gen,
		EventPub: rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{CaseID: "c1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Ledger == nil {
		t.Fatal("no ledger")
	}
	evs := res.Ledger.List()
	if len(evs) != 2 {
		t.Fatalf("expected 2 evidence records (one per Tavily result), got %d", len(evs))
	}
	if evs[0].Observation != "result A content" {
		t.Fatalf("evidence 0 observation: %s", evs[0].Observation)
	}
}

func TestAgentLoop_MaxToolCallsForcesConvergence(t *testing.T) {
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	toolSchema, _ := gen.FromStruct(struct {
		Query string `json:"query"`
	}{})
	toolReg := &stubToolReg{defs: []port.ToolDefinition{{
		Name: "web_search", Desc: "search", ArgsSchema: toolSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"},
	}}}
	toolExec := &stubToolExec{}
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "web_search", `{"query":"a"}`),
			callMsg("c1", "web_search", `{"query":"b"}`),
			callMsg("c1", "web_search", `{"query":"c"}`), // over limit -> forced to summarize
			callMsg("c1", "web_search", `{"query":"d"}`), // over limit -> forced
			finalMsg(summaryJSON("EV-001")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg: toolReg, ToolExec: toolExec, Validator: val, Gen: gen, EventPub: rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.MaxToolCalls = 2
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	completed := 0
	for _, e := range rec.events {
		if e.Type == entity.EventToolCallCompleted {
			completed++
		}
	}
	if completed != 2 {
		t.Fatalf("expected 2 tool-call completions (limit), got %d", completed)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after forced convergence")
	}
}

// validatingScriptedModel wraps scriptedChatModel and asserts every assistant
// tool_call has a matching tool message in the input history - the rule the
// real OpenAI-compatible API enforces (400 "insufficient tool messages
// following tool_calls message" when violated). The plain scriptedChatModel
// ignores its input, so it cannot catch orphaned tool_calls.
type validatingScriptedModel struct {
	inner *scriptedChatModel
	t     *testing.T
}

func (v *validatingScriptedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	pending := map[string]bool{}
	for _, m := range input {
		if m.Role == schema.Assistant {
			for _, tc := range m.ToolCalls {
				pending[tc.ID] = true
			}
		}
		if m.Role == schema.Tool && m.ToolCallID != "" {
			delete(pending, m.ToolCallID)
		}
	}
	if len(pending) > 0 {
		v.t.Fatalf("orphaned assistant tool_calls without matching tool messages: %v", pending)
	}
	return v.inner.Generate(ctx, input, opts...)
}
func (v *validatingScriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("not implemented")
}
func (v *validatingScriptedModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return v, nil
}

func TestAgentLoop_MaxToolCallsNoOrphanedToolCalls(t *testing.T) {
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	toolSchema, _ := gen.FromStruct(struct {
		Query string `json:"query"`
	}{})
	toolReg := &stubToolReg{defs: []port.ToolDefinition{{
		Name: "web_search", Desc: "search", ArgsSchema: toolSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "web_search"},
	}}}
	toolExec := &stubToolExec{}
	rec := &recordingEventPub{}
	inner := &scriptedChatModel{responses: []*schema.Message{
		callMsg("t1", "web_search", `{"query":"a"}`),
		callMsg("t2", "web_search", `{"query":"b"}`),
		callMsg("t3", "web_search", `{"query":"c"}`), // over limit -> forced to summarize
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &validatingScriptedModel{inner: inner, t: t}},
		ToolReg:   toolReg, ToolExec: toolExec, Validator: val, Gen: gen, EventPub: rec,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.MaxToolCalls = 2
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "c1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after forced convergence")
	}
}
