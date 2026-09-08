package runtime_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
	"github.com/jamespud/magi/backend/internal/harnesstest"
)

// TestHarnessTestkit_Smoke verifies the crash-recovery testkit compiles and can
// drive a complete agent loop with tools, invoking repositories and a
// checkpoint store. Task 11 layers crash cases on top of these primitives.
func TestHarnessTestkit_Smoke(t *testing.T) {
	model := &harnesstest.ScriptedModel{Responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}}
	tool := &harnesstest.CountingTool{}
	invocations := harnesstest.NewRecordingInvocationRepository()
	checkpoints := harnesstest.NewFailingCheckpointRepository(nil)
	reg := validation.NewReflectSchemaGenerator()
	calcSchema, err := reg.FromStruct(calcArgs{})
	if err != nil {
		t.Fatalf("calc schema: %v", err)
	}
	registry := &harnesstest.ToolRegistry{Defs: []port.ToolDefinition{{
		Name: "calc", Desc: "add", ArgsSchema: calcSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"},
	}}}

	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &harnesstest.ModelPort{M: model},
		ToolReg:        registry,
		ToolExec:       tool,
		Invocations:    invocations,
		Validator:      validation.NewJSONSchemaValidator(),
		Gen:            reg,
		CheckpointRepo: checkpoints,
	})
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{RunID: "run-toolkit", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status = %v, want completed", res.Status)
	}
	if tool.Calls != 1 {
		t.Fatalf("tool calls = %d, want 1", tool.Calls)
	}
	if _, ok := res.Ledger.Get("EV-001"); !ok {
		t.Fatal("expected evidence EV-001 from the tool result")
	}
	if invocations.BeginAttemptCalls == 0 {
		t.Fatal("expected the tool invocation to be fenced through the kernel")
	}
}
