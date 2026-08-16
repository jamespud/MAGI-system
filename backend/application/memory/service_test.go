package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubMemoryRepo struct {
	proj *entity.CaseMemoryProjection
}

func (s *stubMemoryRepo) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	return s.proj, nil
}
func (s *stubMemoryRepo) Save(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	s.proj = proj
	return nil
}
func (s *stubMemoryRepo) Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}

func TestMemoryService_Get(t *testing.T) {
	want := &entity.CaseMemoryProjection{QuestionSummary: "test"}
	repo := &stubMemoryRepo{proj: want}
	svc := memory.NewService(nil, repo)
	got, err := svc.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.QuestionSummary != "test" {
		t.Fatalf("expected test, got: %+v", got)
	}
}

func TestMemoryService_Store(t *testing.T) {
	repo := &stubMemoryRepo{}
	svc := memory.NewService(nil, repo)
	proj := &entity.CaseMemoryProjection{QuestionSummary: "stored"}
	err := svc.Store(context.Background(), proj)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if repo.proj == nil || repo.proj.QuestionSummary != "stored" {
		t.Fatal("projection not stored")
	}
}

type searchMemRepo struct {
	results []*entity.CaseMemoryProjection
}

func (s *searchMemRepo) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	return nil, nil
}
func (s *searchMemRepo) Save(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	return nil
}
func (s *searchMemRepo) Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	return s.results, nil
}

type memCaseRepo struct {
	byID map[string]*entity.DecisionCase
}

func (s *memCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *memCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.byID[id], nil
}
func (s *memCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *memCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *memCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

func TestMemoryService_SearchFiltersByOwner(t *testing.T) {
	repo := &searchMemRepo{results: []*entity.CaseMemoryProjection{{CaseID: "c1"}, {CaseID: "c2"}}}
	cases := &memCaseRepo{byID: map[string]*entity.DecisionCase{
		"c1": {ID: "c1", UserID: 7},
		"c2": {ID: "c2", UserID: 8},
	}}
	svc := memory.NewService(nil, repo, memory.WithCaseRepo(cases))
	out, err := svc.Search(context.Background(), 7, "history", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) != 1 || out[0].CaseID != "c1" {
		t.Fatalf("owner filter: %+v", out)
	}
}

// --- semantic search stubs ---

type stubKnowledge struct {
	blocks []port.MergedBlock
	err    error
	gotReq port.RetrieveRequest
}

func (k *stubKnowledge) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	k.gotReq = req
	return port.RetrieveResult{Blocks: k.blocks}, k.err
}
func (k *stubKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	return port.StoreStats{}, nil
}

type mapMemRepo struct {
	byID    map[string]*entity.CaseMemoryProjection
	like    []*entity.CaseMemoryProjection
	likeErr error
}

func (s *mapMemRepo) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	return s.byID[caseID], nil
}
func (s *mapMemRepo) Save(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	return nil
}
func (s *mapMemRepo) Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	return s.like, s.likeErr
}

func TestMemoryService_SearchFusesSemanticThenLike(t *testing.T) {
	repo := &mapMemRepo{
		byID: map[string]*entity.CaseMemoryProjection{
			"case-a": {CaseID: "case-a"},
			"case-b": {CaseID: "case-b"},
		},
		like: []*entity.CaseMemoryProjection{{CaseID: "case-c"}},
	}
	know := &stubKnowledge{blocks: []port.MergedBlock{{SourceRef: "case-a"}, {SourceRef: "case-b"}}}
	svc := memory.NewService(know, repo)

	out, err := svc.Search(context.Background(), 0, "db migration strategy", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	want := []string{"case-a", "case-b", "case-c"}
	if len(out) != len(want) {
		t.Fatalf("got %d results, want %d: %+v", len(out), len(want), out)
	}
	for i, id := range want {
		if out[i].CaseID != id {
			t.Fatalf("result[%d] = %s, want %s", i, out[i].CaseID, id)
		}
	}
	// The RAG call must be scoped to case-memory source so documents (D7)
	// never leak into historical-memory search.
	if len(know.gotReq.Sources) != 1 || know.gotReq.Sources[0] != "case_memory" {
		t.Fatalf("retrieve sources = %v, want [case_memory]", know.gotReq.Sources)
	}
	if know.gotReq.TopK <= 0 {
		t.Fatalf("expected positive TopK, got %d", know.gotReq.TopK)
	}
}

func TestMemoryService_SearchDegradesToLikeOnRetrievalError(t *testing.T) {
	know := &stubKnowledge{err: errors.New("milvus down")}
	repo := &mapMemRepo{like: []*entity.CaseMemoryProjection{{CaseID: "case-x"}}}
	svc := memory.NewService(know, repo)

	out, err := svc.Search(context.Background(), 0, "q", 5)
	if err != nil {
		t.Fatalf("search should not fail on retrieval error: %v", err)
	}
	if len(out) != 1 || out[0].CaseID != "case-x" {
		t.Fatalf("expected LIKE fallback result, got %+v", out)
	}
}

func TestMemoryService_SearchDedupsSemanticAndLike(t *testing.T) {
	repo := &mapMemRepo{
		byID: map[string]*entity.CaseMemoryProjection{"case-a": {CaseID: "case-a"}},
		like: []*entity.CaseMemoryProjection{{CaseID: "case-a"}, {CaseID: "case-b"}},
	}
	know := &stubKnowledge{blocks: []port.MergedBlock{{SourceRef: "case-a"}}}
	svc := memory.NewService(know, repo)

	out, err := svc.Search(context.Background(), 0, "q", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) != 2 || out[0].CaseID != "case-a" || out[1].CaseID != "case-b" {
		t.Fatalf("dedup failed: %+v", out)
	}
}

func TestMemoryService_SearchOwnerFiltersSemanticHits(t *testing.T) {
	repo := &mapMemRepo{
		byID: map[string]*entity.CaseMemoryProjection{
			"case-a": {CaseID: "case-a"},
			"case-b": {CaseID: "case-b"},
		},
		like: []*entity.CaseMemoryProjection{{CaseID: "case-c"}},
	}
	know := &stubKnowledge{blocks: []port.MergedBlock{{SourceRef: "case-a"}, {SourceRef: "case-b"}}}
	cases := &memCaseRepo{byID: map[string]*entity.DecisionCase{
		"case-a": {ID: "case-a", UserID: 7},
		"case-b": {ID: "case-b", UserID: 8},
		"case-c": {ID: "case-c", UserID: 7},
	}}
	svc := memory.NewService(know, repo, memory.WithCaseRepo(cases))

	out, err := svc.Search(context.Background(), 7, "q", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) != 2 || out[0].CaseID != "case-a" || out[1].CaseID != "case-c" {
		t.Fatalf("owner filter on semantic+like wrong: %+v", out)
	}
}

func TestMemoryService_SearchSurfacesLikeErrorWhenNoSemantic(t *testing.T) {
	know := &stubKnowledge{err: errors.New("milvus down")}
	repo := &mapMemRepo{likeErr: errors.New("db down")}
	svc := memory.NewService(know, repo)

	_, err := svc.Search(context.Background(), 0, "q", 5)
	if err == nil {
		t.Fatal("expected LIKE error to surface when semantic produced nothing")
	}
}

func TestMemoryService_SearchEmptyQuery(t *testing.T) {
	know := &stubKnowledge{blocks: []port.MergedBlock{{SourceRef: "case-a"}}}
	repo := &mapMemRepo{byID: map[string]*entity.CaseMemoryProjection{"case-a": {CaseID: "case-a"}}}
	svc := memory.NewService(know, repo)

	out, err := svc.Search(context.Background(), 0, "   ", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected no results for empty query, got %+v", out)
	}
}
