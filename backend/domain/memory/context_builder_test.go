package memory_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type mockKnowledge struct{ blocks []port.MergedBlock }

func (m *mockKnowledge) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{Blocks: m.blocks}, nil
}
func (m *mockKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	return port.StoreStats{}, nil
}

func TestContextBuilder_WithKnowledge(t *testing.T) {
	b := memory.NewContextBuilder(&mockKnowledge{blocks: []port.MergedBlock{
		{Level: 300, Content: "hist", SourceRef: "case-old"},
	}})
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
	if actx.KnowledgeCtx[0].SourceURI != "case-old" {
		t.Fatalf("source uri: %q", actx.KnowledgeCtx[0].SourceURI)
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

type failingKnowledge struct{}

func (f *failingKnowledge) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{}, fmt.Errorf("milvus down")
}
func (f *failingKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	return port.StoreStats{}, nil
}

type captureEventPublisher struct {
	events []entity.MagiEvent
}

func (p *captureEventPublisher) Publish(ctx context.Context, e entity.MagiEvent) error {
	p.events = append(p.events, e)
	return nil
}

// TestContextBuilder_SurfacesRetrievalFailure verifies a RAG failure is NOT
// silently swallowed: it logs, increments the metric and publishes a
// MEMORY_RETRIEVAL_FAILED event (P0: D3).
func TestContextBuilder_SurfacesRetrievalFailure(t *testing.T) {
	reg := metrics.New()
	pub := &captureEventPublisher{}
	b := memory.NewContextBuilder(&failingKnowledge{}, memory.WithMetrics(reg), memory.WithEventPublisher(pub))
	actx, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q"},
		&entity.DecisionTask{CanonicalQuestion: "compute"},
		nil, nil)
	if err != nil {
		t.Fatalf("build should not fail even when retrieval fails: %v", err)
	}
	if len(actx.KnowledgeCtx) != 0 {
		t.Fatalf("expected no knowledge chunks on failure, got %+v", actx.KnowledgeCtx)
	}
	if reg.MemoryRetrievalFailures.Load() != 1 {
		t.Fatalf("expected 1 retrieval failure metric, got %d", reg.MemoryRetrievalFailures.Load())
	}
	if len(pub.events) != 1 || pub.events[0].Type != entity.EventMemoryRetrievalFailed || pub.events[0].CaseID != "c1" {
		t.Fatalf("expected MEMORY_RETRIEVAL_FAILED event for c1, got %+v", pub.events)
	}
}

type captureRetrieve struct {
	last   port.RetrieveRequest
	blocks []port.MergedBlock
}

func (c *captureRetrieve) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	c.last = req
	return port.RetrieveResult{Blocks: c.blocks}, nil
}
func (c *captureRetrieve) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	return port.StoreStats{}, nil
}

func TestContextBuilder_AppendsBackgroundToQueries(t *testing.T) {
	k := &captureRetrieve{blocks: []port.MergedBlock{{Level: 300, Content: "hist", SourceRef: "case-old"}}}
	b := memory.NewContextBuilder(k)
	_, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q", Context: "bg text"},
		&entity.DecisionTask{CanonicalQuestion: "compute"},
		nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(k.last.Queries) != 2 || k.last.Queries[0] != "compute" || k.last.Queries[1] != "bg text" {
		t.Fatalf("queries = %v, want [compute bg text]", k.last.Queries)
	}
}

func TestContextBuilder_NoBackgroundKeepsSingleQuery(t *testing.T) {
	k := &captureRetrieve{}
	b := memory.NewContextBuilder(k)
	_, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q"},
		&entity.DecisionTask{CanonicalQuestion: "compute"},
		nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(k.last.Queries) != 1 || k.last.Queries[0] != "compute" {
		t.Fatalf("queries = %v, want [compute]", k.last.Queries)
	}
}

func TestContextBuilder_TruncatesLongBackground(t *testing.T) {
	long := strings.Repeat("长", 3000)
	k := &captureRetrieve{}
	b := memory.NewContextBuilder(k)
	_, err := b.Build(context.Background(),
		&entity.DecisionCase{ID: "c1", Question: "q", Context: long},
		&entity.DecisionTask{CanonicalQuestion: "compute"},
		nil, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(k.last.Queries) != 2 || len([]rune(k.last.Queries[1])) != 2000 {
		t.Fatalf("background query runes = %d, want 2000", len([]rune(k.last.Queries[1])))
	}
}
