package runtime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

// capturingChatModel records every Generate input so the test can assert the
// deterministic check feedback reached the model.
type capturingChatModel struct {
	inner     *scriptedChatModel
	generated [][]*schema.Message
}

func (c *capturingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	c.generated = append(c.generated, input)
	return c.inner.Generate(ctx, input, opts...)
}

func (c *capturingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return c.inner.Stream(ctx, input, opts...)
}

func (c *capturingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return c, nil
}

type capturingModelPort struct{ m model.ToolCallingChatModel }

func (s *capturingModelPort) Build(ctx context.Context, ref entity.ModelRef) (model.ToolCallingChatModel, error) {
	return s.m, nil
}

func TestAgentLoop_FeedbackSensorFeedsBackAndSelfHeals(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(struct {
		A int `json:"a"`
		B int `json:"b"`
	}{})
	checkSchema := []byte(`{"type":"object","properties":{"payload":{},"rules":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"op":{"type":"string"},"value":{}},"required":["field","op"]}}},"required":["payload"],"additionalProperties":false}`)

	captured := &capturingChatModel{inner: &scriptedChatModel{responses: []*schema.Message{
		callMsg("c1", "check_output", `{"payload":{"decision":"maybe"},"rules":[{"field":"decision","op":"eq","value":"approve"}]}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}}}

	sensors := runtime.NewCompositeFeedbackSensor(runtime.NewConstraintFeedbackSensor())
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &capturingModelPort{m: captured},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{
			{Name: "check_output", Desc: "lint", ArgsSchema: checkSchema, Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "check_output"}},
			{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}},
		}},
		ToolExec:  magi.NewFeedbackToolExecutor(sensors, nil),
		Validator: v, Gen: gen,
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}

	cfg := evidenceCfg(1, 0)
	cfg.Tools = []entity.ToolBinding{
		{Source: entity.ToolSourceLocal, ToolName: "check_output"},
		{Source: entity.ToolSourceLocal, ToolName: "calc"},
	}
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(captured.generated) < 2 {
		t.Fatalf("expected feedback to reach the model, generated=%d", len(captured.generated))
	}
	var sawViolation bool
	for _, msg := range captured.generated[1] {
		if msg.Role == schema.Tool && strings.Contains(msg.Content, `must equal approve`) {
			sawViolation = true
			break
		}
	}
	if !sawViolation {
		t.Fatalf("check_output violation was not fed back to the model")
	}
}
