package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
)

type reportScriptedModel struct {
	responses []*schema.Message
	calls     int
}

func (s *reportScriptedModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if s.calls >= len(s.responses) {
		return schema.AssistantMessage("not json", nil), nil
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}
func (s *reportScriptedModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}
func (s *reportScriptedModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return s, nil
}

type reportStubModelPort struct{ m model.ToolCallingChatModel }

func (p *reportStubModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return p.m, nil
}

func newReportCommander(t *testing.T, m model.ToolCallingChatModel) *service.Commander {
	t.Helper()
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	cmd, err := service.NewCommander(service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"}, &reportStubModelPort{m: m}, gen, val)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}
	return cmd
}

func TestGenerateReport_ValidJSONRenders(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"decision":"approve","summary":"proceed","key_reasons":["r1"],"risks":[],"next_steps":["s1"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	out, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"}, &entity.Resolution{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	for _, want := range []string{"proceed", "approve", "- r1", "- s1"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered report missing %q, got:\n%s", want, out)
		}
	}
}

func TestGenerateReport_InvalidJSONRetriesAndFails(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage("not json", nil),
		schema.AssistantMessage("still not json", nil),
		schema.AssistantMessage("{invalid", nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"}, &entity.Resolution{}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error after 3 invalid attempts")
	}
}

func TestGenerateReport_RequiresEvidenceCitation(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"decision":"approve","summary":"no citation","key_reasons":["r1"],"risks":[],"next_steps":["s1"]}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"cited","key_reasons":["r1"],"risks":[],"next_steps":["s1"],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	out, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"}, &entity.Resolution{}, nil, []string{"EV-001"}, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if m.calls != 2 {
		t.Fatalf("expected retry after missing citation, calls=%d", m.calls)
	}
	if !strings.Contains(out, "EV-001") {
		t.Fatalf("rendered report missing cited evidence ID, got:\n%s", out)
	}
}

func TestGenerateReport_FailsWhenCitationNeverProvided(t *testing.T) {
	m := &reportScriptedModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"decision":"approve","summary":"a","key_reasons":["r"],"risks":[],"next_steps":["s"]}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"b","key_reasons":["r"],"risks":[],"next_steps":["s"]}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"c","key_reasons":["r"],"risks":[],"next_steps":["s"]}`, nil),
	}}
	cmd := newReportCommander(t, m)
	_, err := cmd.GenerateReport(context.Background(), &entity.DecisionCase{Question: "q"}, &entity.Resolution{}, nil, []string{"EV-001"}, nil)
	if err == nil {
		t.Fatal("expected error when report never cites available evidence")
	}
}
