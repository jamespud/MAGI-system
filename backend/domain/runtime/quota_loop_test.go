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

type fakeQuota struct {
	limit int
	calls int
}

func (q *fakeQuota) Allow(ctx context.Context, userID, toolName string) (bool, error) {
	q.calls++
	return q.calls <= q.limit, nil
}

func TestAgentLoop_ToolQuotaDeniesExcessCalls(t *testing.T) {
	v := validation.NewJSONSchemaValidator()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, _ := gen.FromStruct(calcArgs{})
	binding := entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}
	quota := &fakeQuota{limit: 1}
	rec := &recordingEventPub{}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: &stubModelPort{m: &scriptedChatModel{responses: []*schema.Message{
			callMsg("c1", "calc", `{"a":1,"b":2}`),
			callMsg("c1", "calc", `{"a":1,"b":2}`),
			finalMsg(summaryJSON("EV-001")),
			finalMsg(voteJSON("correctness")),
		}}},
		ToolReg: &stubToolReg{defs: []port.ToolDefinition{{
			Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: binding,
		}}},
		ToolExec: &stubToolExec{}, Validator: v, Gen: gen, EventPub: rec,
		Quota: quota,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{CaseID: "c1", UserID: "7", RunID: "q1", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Vote == nil {
		t.Fatal("expected a vote after quota denial")
	}
	denied := 0
	for _, st := range res.Trace.Steps {
		for _, tc := range st.ToolCalls {
			if contains(tc.Err, "quota exceeded") {
				denied++
			}
		}
	}
	if denied != 1 {
		t.Fatalf("expected 1 quota denial, got %d", denied)
	}
	if quota.calls != 2 {
		t.Fatalf("quota calls: %d", quota.calls)
	}
}
