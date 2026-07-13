package memory_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type mockKnowledge struct{ chunks []port.KnowledgeChunk }

func (m *mockKnowledge) Retrieve(ctx context.Context, query string, ids []int64) ([]port.KnowledgeChunk, error) {
	return m.chunks, nil
}
func (m *mockKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error { return nil }

func TestContextBuilder_WithKnowledge(t *testing.T) {
	b := memory.NewContextBuilder(&mockKnowledge{chunks: []port.KnowledgeChunk{{Content: "hist", Score: 0.9}}})
	actx, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q"},
		&entity.DecisionTask{CanonicalQuestion: "compute"},
		nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if actx.CaseID != "c1" {
		t.Fatalf("caseID: %s", actx.CaseID)
	}
	if len(actx.KnowledgeCtx) != 1 || actx.KnowledgeCtx[0].Content != "hist" {
		t.Fatalf("knowledge: %+v", actx.KnowledgeCtx)
	}
	if actx.Task.CanonicalQuestion != "compute" {
		t.Fatalf("task: %+v", actx.Task)
	}
}

func TestContextBuilder_NoKnowledge(t *testing.T) {
	b := memory.NewContextBuilder(nil)
	actx, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if actx.KnowledgeCtx != nil {
		t.Fatalf("expected nil knowledge")
	}
}

func TestContextBuilder_WithDebate(t *testing.T) {
	b := memory.NewContextBuilder(nil)
	dc := &runtime.DebateContext{}
	actx, _ := b.Build(context.Background(), &entity.DecisionCase{ID: "c1"}, nil, dc, nil)
	if actx.DebateContext == nil {
		t.Fatalf("expected debate context")
	}
}
