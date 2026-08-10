package runtime_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

// TestAgentLoop_PassesArgsSchemaToModel guards against the tool schema being
// dropped when building ToolInfo: with ParamsOneOf=nil the model believes the
// tool takes no arguments and sends empty payloads, which then fail runtime
// validation (observed repeatedly with MCP tools requiring "symbol").
func TestAgentLoop_PassesArgsSchemaToModel(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	// Inline JSON Schema, the same shape MCP servers and local tools emit
	// (web_search, code_runner): properties + required at the top level.
	calcSchema := []byte(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"required":["a","b"],"additionalProperties":false}`)
	cm := &scriptedChatModel{responses: []*schema.Message{
		schema.AssistantMessage(summaryJSON("EV-001"), nil),
		schema.AssistantMessage(voteJSON("correctness"), nil),
	}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: cm},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{{
			Name: "calc", Desc: "add two numbers", ArgsSchema: calcSchema,
			Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"},
		}}},
		ToolExec:  &stubToolExec{},
		Validator: v, Gen: gen,
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}
	cfg := evidenceCfg(0, 0)
	if _, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{
		CaseID: "c", Task: entity.DecisionTask{CanonicalQuestion: "q"},
		ToolBindings: []entity.ToolBinding{{Source: entity.ToolSourceLocal, ToolName: "calc"}},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(cm.tools) != 1 {
		t.Fatalf("expected 1 tool bound, got %d", len(cm.tools))
	}
	info := cm.tools[0]
	if info.ParamsOneOf == nil {
		t.Fatal("ToolInfo.ParamsOneOf is nil: model cannot see required parameters")
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("to json schema: %v", err)
	}
	if js == nil || len(js.Required) == 0 {
		t.Fatalf("expected required parameters in tool schema, got %+v", js)
	}
	seen := map[string]bool{}
	for _, r := range js.Required {
		seen[r] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("required params missing: %v", js.Required)
	}
}
