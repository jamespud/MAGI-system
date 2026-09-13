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

// Two sources can still describe the same model-facing name; the loop must bind
// exactly one definition instead of overwriting silently and sending the
// provider a tool list with duplicate function names.
func TestAgentLoop_DeduplicatesToolNames(t *testing.T) {
	def := func(source entity.ToolSource, tool string) port.ToolDefinition {
		return port.ToolDefinition{
			Name:       "duplicate_tool",
			Desc:       string(source),
			ArgsSchema: []byte(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"],"additionalProperties":false}`),
			Source:     source,
			Binding:    entity.ToolBinding{Source: source, ToolName: tool},
		}
	}
	cm := &scriptedChatModel{responses: []*schema.Message{
		schema.AssistantMessage(summaryJSON("EV-001"), nil),
		schema.AssistantMessage(voteJSON("correctness"), nil),
	}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: cm},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{
			def(entity.ToolSourceLocal, "duplicate_tool"),
			def(entity.ToolSourcePlugin, "duplicate_tool"),
		}},
		ToolExec:  &stubToolExec{},
		Validator: validation.NewJSONSchemaValidator(),
		Gen:       validation.NewReflectSchemaGenerator(),
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}
	if _, err := loop.Run(context.Background(), evidenceCfg(0, 0), &runtime.AgentContext{
		CaseID: "c", Task: entity.DecisionTask{CanonicalQuestion: "q"},
		ToolBindings: []entity.ToolBinding{
			{Source: entity.ToolSourceLocal, ToolName: "duplicate_tool"},
			{Source: entity.ToolSourcePlugin, PluginID: 1, ToolID: 2},
		},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(cm.tools) != 1 {
		t.Fatalf("bound %d tools, want 1 after deduplication", len(cm.tools))
	}
	if cm.tools[0].Name != "duplicate_tool" {
		t.Fatalf("bound tool = %q", cm.tools[0].Name)
	}
}
