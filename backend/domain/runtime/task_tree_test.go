package runtime_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

type recordingTaskTree struct {
	calls []string
}

func (r *recordingTaskTree) RecordAgent(ctx context.Context, caseID, runID, agentCode, status string) error {
	r.calls = append(r.calls, caseID+"|"+agentCode+"|"+status)
	return nil
}

func TestAgentLoop_RecordsTaskTreeNode(t *testing.T) {
	tree := &recordingTaskTree{}
	loop := newAgentLoopWithTree(t, []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}, tree)
	cfg := evidenceCfg(1, 0)
	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{
		CaseID: "case-1", RunID: "run-1",
		Task: entity.DecisionTask{CanonicalQuestion: "compute"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status: %v", res.Status)
	}
	if len(tree.calls) != 1 || tree.calls[0] != "case-1|melchior|completed" {
		t.Fatalf("task tree calls = %+v", tree.calls)
	}
}

func newAgentLoopWithTree(t *testing.T, responses []*schema.Message, tree *recordingTaskTree) *runtime.AgentLoop {
	t.Helper()
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: responses}},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{
			{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding},
		}},
		ToolExec:  &stubToolExec{},
		Validator: v, Gen: gen,
		Adapter: evidence.NewEvidenceAdapterRegistry(
			evidence.FullReliabilityResolver(),
			evidence.NewWebSearchAdapter(),
			evidence.NewNativeAdapter(),
			evidence.NewRawObservationAdapter(),
		),
		ToolPolicy: toolpolicy.NewPolicy([]string{"code_runner"}, nil),
		TaskTree:   tree,
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}
	return loop
}
