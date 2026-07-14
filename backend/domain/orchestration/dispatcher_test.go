package orchestration_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type captureRuntime struct {
	actx *runtime.AgentContext
}

func (c *captureRuntime) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	c.actx = actx
	return &runtime.LoopResult{Status: runtime.LoopStatusCompleted}, nil
}

type mockKnowledgePort struct {
	chunks []port.KnowledgeChunk
	stored []*entity.CaseMemoryProjection
}

func (m *mockKnowledgePort) Retrieve(ctx context.Context, query string, knowledgeIDs []int64) ([]port.KnowledgeChunk, error) {
	return m.chunks, nil
}
func (m *mockKnowledgePort) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	m.stored = append(m.stored, proj)
	return nil
}

func TestDispatch_SetsInvestigateRunID(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr, nil)
	cfg := &entity.MagiConfig{Code: "melchior"}
	d.Dispatch(context.Background(), &entity.DecisionCase{ID: "c1"}, nil, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c1-melchior-r1-investigate"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}
}

func TestDispatchReconsider_SetsReconsiderRunID(t *testing.T) {
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr, nil)
	cfg := &entity.MagiConfig{Code: "balthasar"}
	d.DispatchReconsider(context.Background(), &entity.DecisionCase{ID: "c2"}, nil, entity.DebatePacket{}, nil, []*entity.MagiConfig{cfg}, 2)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	want := "c2-balthasar-r2-reconsider"
	if cr.actx.RunID != want {
		t.Fatalf("RunID=%q want=%q", cr.actx.RunID, want)
	}
}

func TestDispatch_ContextBuilderRetrievesKnowledge(t *testing.T) {
	kp := &mockKnowledgePort{chunks: []port.KnowledgeChunk{{Content: "historical case X"}}}
	cb := memory.NewContextBuilder(kp)
	cr := &captureRuntime{}
	d := orchestration.NewDispatcher(cr, cb)
	cfg := &entity.MagiConfig{Code: "melchior"}
	d.Dispatch(context.Background(), &entity.DecisionCase{ID: "c1"}, &entity.DecisionTask{CanonicalQuestion: "q"}, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	if len(cr.actx.KnowledgeCtx) == 0 {
		t.Fatal("expected KnowledgeCtx populated by ContextBuilder")
	}
	if cr.actx.KnowledgeCtx[0].Content != "historical case X" {
		t.Fatalf("knowledge content: %s", cr.actx.KnowledgeCtx[0].Content)
	}
}
