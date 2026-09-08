package decision_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/modelruntime"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/toolruntime"
	"github.com/jamespud/magi/backend/domain/validation"
	"github.com/jamespud/magi/backend/internal/harnesstest"
)

// leaseCancelModel wraps a scripted model and, after producing a response,
// cancels the run context with ErrLeaseLost to simulate the worker losing its
// job lease mid-generation.
type leaseCancelModel struct {
	inner  *harnesstest.ScriptedModel
	cancel context.CancelCauseFunc
}

func (m *leaseCancelModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	msg, err := m.inner.Generate(ctx, input, opts...)
	if err == nil && m.cancel != nil {
		m.cancel(port.ErrLeaseLost)
	}
	return msg, err
}

func (m *leaseCancelModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("lease cancel model: streaming not implemented")
}

func (m *leaseCancelModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

var _ model.ToolCallingChatModel = (*leaseCancelModel)(nil)

func TestLeaseLostPreventsLateInvocationCommit(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	invocations := harnesstest.NewRecordingInvocationRepository()
	checkpoints := harnesstest.NewFailingCheckpointRepository(nil)
	val := validation.NewJSONSchemaValidator()
	reg := validation.NewReflectSchemaGenerator()
	calcSchema, err := reg.FromStruct(struct {
		A int `json:"a"`
		B int `json:"b"`
	}{})
	if err != nil {
		t.Fatalf("calc schema: %v", err)
	}
	toolCall := schema.AssistantMessage("", []schema.ToolCall{{
		ID: "c1", Type: "function",
		Function: schema.FunctionCall{Name: "calc", Arguments: `{"a":1,"b":2}`},
	}})
	model := &harnesstest.ScriptedModel{Responses: []*schema.Message{toolCall}}
	modelRuntime := modelruntime.New(execution.NewKernel(invocations, nil))
	toolRuntime, err := toolruntime.New(toolruntime.Deps{
		Kernel:    execution.NewKernel(invocations, nil),
		Executor:  &harnesstest.CountingTool{},
		Validator: val,
	})
	if err != nil {
		t.Fatalf("tool runtime: %v", err)
	}
	registry := &harnesstest.ToolRegistry{Defs: []port.ToolDefinition{{
		Name: "calc", Desc: "add", ArgsSchema: calcSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"},
	}}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &harnesstest.ModelPort{M: &leaseCancelModel{inner: model, cancel: cancel}},
		ModelRuntime:   modelRuntime,
		ToolReg:        registry,
		ToolRuntime:    toolRuntime,
		Validator:      val,
		Gen:            reg,
		CheckpointRepo: checkpoints,
	})
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}
	cfg := &entity.MagiConfig{
		Code: "melchior", Persona: "scientist",
		Model:      entity.ModelRef{ModelID: 1},
		Tools:      []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
		LoopPolicy: entity.LoopPolicy{MaxSteps: 12, MaxToolCalls: 5},
	}

	_, runErr := loop.Run(ctx, cfg, &runtime.AgentContext{RunID: "run-lease", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if runErr == nil {
		t.Fatal("want ErrLeaseLost when the job lease is lost")
	}
	if !errors.Is(runErr, port.ErrLeaseLost) && !errors.Is(context.Cause(ctx), port.ErrLeaseLost) {
		t.Fatalf("run error = %v, want ErrLeaseLost", runErr)
	}
	// The pre-generation checkpoint save is allowed, but the checkpoint that
	// would capture the model response after the lease was lost must be refused.
	if checkpoints.Saves > 1 {
		t.Fatalf("checkpoint saves = %d, want at most 1 (no late post-model commit)", checkpoints.Saves)
	}
}
