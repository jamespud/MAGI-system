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
	blocks []port.MergedBlock
	stored []*entity.CaseMemoryProjection
}

func (m *mockKnowledgePort) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{Blocks: m.blocks}, nil
}
func (m *mockKnowledgePort) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	m.stored = append(m.stored, proj)
	return port.StoreStats{}, nil
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

type stubBindingsProvider struct {
	userID   int64
	bindings []entity.ToolBinding
}

func (s *stubBindingsProvider) BindingsForUser(ctx context.Context, userID int64) ([]entity.ToolBinding, error) {
	s.userID = userID
	return s.bindings, nil
}

func TestDispatch_InjectsUserToolBindings(t *testing.T) {
	cr := &captureRuntime{}
	prov := &stubBindingsProvider{bindings: []entity.ToolBinding{{Source: entity.ToolSourcePlugin, PluginID: 1, ToolID: 2}}}
	d := orchestration.NewDispatcher(cr, nil, orchestration.WithToolBindingsProvider(prov))
	cfg := &entity.MagiConfig{Code: "melchior"}
	d.Dispatch(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 7}, nil, []*entity.MagiConfig{cfg}, 1)
	if cr.actx == nil {
		t.Fatal("no actx captured")
	}
	if prov.userID != 7 {
		t.Fatalf("provider userID: %d", prov.userID)
	}
	if len(cr.actx.ToolBindings) != 1 || cr.actx.ToolBindings[0].PluginID != 1 {
		t.Fatalf("tool bindings: %+v", cr.actx.ToolBindings)
	}
}

func TestDispatch_ContextBuilderRetrievesKnowledge(t *testing.T) {
	kp := &mockKnowledgePort{blocks: []port.MergedBlock{{Level: 300, Content: "historical case X"}}}
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
