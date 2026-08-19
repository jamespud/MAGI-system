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
func (s *stubMemoryRepo) List(ctx context.Context) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}
func (s *stubMemoryRepo) Delete(ctx context.Context, caseID string) error { return nil }

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
func (s *searchMemRepo) List(ctx context.Context) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}
func (s *searchMemRepo) Delete(ctx context.Context, caseID string) error { return nil }

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
func (s *memCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *memCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *memCaseRepo) Delete(ctx context.Context, id string) error { return nil }

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
func (s *mapMemRepo) Delete(ctx context.Context, caseID string) error {
	delete(s.byID, caseID)
	return nil
}
func (s *mapMemRepo) List(ctx context.Context) ([]*entity.CaseMemoryProjection, error) {
	if s.byID == nil {
		return nil, nil
	}
	out := make([]*entity.CaseMemoryProjection, 0, len(s.byID))
	for _, v := range s.byID {
		out = append(out, v)
	}
	return out, nil
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

type mutableMemoryRepo struct {
	byID map[string]*entity.CaseMemoryProjection
}

func cloneMemoryProjection(in *entity.CaseMemoryProjection) *entity.CaseMemoryProjection {
	if in == nil {
		return nil
	}
	out := *in
	out.KeyEvidence = append([]entity.MemoryEvidence(nil), in.KeyEvidence...)
	out.KeyClaims = append([]entity.MemoryClaim(nil), in.KeyClaims...)
	out.Votes = append([]entity.MemoryVote(nil), in.Votes...)
	out.Tags = append([]string(nil), in.Tags...)
	if in.Outcome != nil {
		outcome := *in.Outcome
		out.Outcome = &outcome
	}
	return &out
}

func (s *mutableMemoryRepo) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	if proj, ok := s.byID[caseID]; ok {
		return cloneMemoryProjection(proj), nil
	}
	return nil, nil
}

func (s *mutableMemoryRepo) Save(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	s.byID[proj.CaseID] = cloneMemoryProjection(proj)
	return nil
}

func (s *mutableMemoryRepo) Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}

func (s *mutableMemoryRepo) List(ctx context.Context) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}

func (s *mutableMemoryRepo) Delete(ctx context.Context, caseID string) error {
	delete(s.byID, caseID)
	return nil
}

type recordingMemoryIndexer struct {
	stored   []*entity.CaseMemoryProjection
	deleted  []string
	storeErr error
}

func (i *recordingMemoryIndexer) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{}, nil
}

func (i *recordingMemoryIndexer) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	if i.storeErr != nil {
		return port.StoreStats{}, i.storeErr
	}
	i.stored = append(i.stored, cloneMemoryProjection(proj))
	return port.StoreStats{Chunks300: 1}, nil
}

func (i *recordingMemoryIndexer) DeleteSource(ctx context.Context, source, sourceRef string) error {
	i.deleted = append(i.deleted, source+":"+sourceRef)
	return nil
}

func TestMemoryService_UpdateEditsAnnotatesTagsAndReindexes(t *testing.T) {
	repo := &mutableMemoryRepo{byID: map[string]*entity.CaseMemoryProjection{
		"c1": {CaseID: "c1", QuestionSummary: "old q", ContextSummary: "old ctx", Resolution: "old", Outcome: &entity.CaseOutcome{Status: "resolved"}},
	}}
	indexer := &recordingMemoryIndexer{}
	svc := memory.NewService(nil, repo, memory.WithIndexer(indexer))

	question, contextSummary, resolution, learned, annotation := "new q", "new ctx", "new result", "new lesson", "trusted memory"
	got, err := svc.Update(context.Background(), 0, "c1", memory.UpdatePatch{
		QuestionSummary: &question,
		ContextSummary:  &contextSummary,
		Resolution:      &resolution,
		Learned:         &learned,
		Annotation:      &annotation,
		Tags:            []string{"ops", "Ops", "database"},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.QuestionSummary != question || got.ContextSummary != contextSummary || got.Resolution != resolution ||
		got.Outcome.Learned != learned || got.Annotation != annotation || len(got.Tags) != 2 {
		t.Fatalf("updated projection: %+v", got)
	}
	if len(indexer.stored) != 1 || indexer.stored[0].Annotation != annotation {
		t.Fatalf("reindex calls: %+v", indexer.stored)
	}
}

func TestMemoryService_UpdateRejectsOtherOwner(t *testing.T) {
	repo := &mutableMemoryRepo{byID: map[string]*entity.CaseMemoryProjection{"c1": {CaseID: "c1"}}}
	indexer := &recordingMemoryIndexer{}
	cases := &memCaseRepo{byID: map[string]*entity.DecisionCase{"c1": {ID: "c1", UserID: 8}}}
	svc := memory.NewService(nil, repo, memory.WithCaseRepo(cases), memory.WithIndexer(indexer))

	annotation := "not mine"
	if _, err := svc.Update(context.Background(), 7, "c1", memory.UpdatePatch{Annotation: &annotation}); err != memory.ErrForbidden {
		t.Fatalf("error=%v, want forbidden", err)
	}
	if len(indexer.stored) != 0 {
		t.Fatal("forbidden update must not reindex")
	}
}

func TestMemoryService_DeleteRemovesAndRestoresOnIndexFailure(t *testing.T) {
	repo := &mutableMemoryRepo{byID: map[string]*entity.CaseMemoryProjection{"c1": {CaseID: "c1", QuestionSummary: "kept"}}}
	indexer := &recordingMemoryIndexer{}
	svc := memory.NewService(nil, repo, memory.WithIndexer(indexer))
	if err := svc.Delete(context.Background(), 0, "c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, exists := repo.byID["c1"]; exists || len(indexer.deleted) != 1 || indexer.deleted[0] != "case_memory:c1" {
		t.Fatalf("delete state: repo=%v deleted=%v", repo.byID, indexer.deleted)
	}

	repo.byID["c2"] = &entity.CaseMemoryProjection{
		CaseID: "c2", QuestionSummary: "kept",
		Outcome: &entity.CaseOutcome{Learned: "old lesson"},
		Tags:    []string{"old"},
	}
	failing := &recordingMemoryIndexer{storeErr: errors.New("index unavailable")}
	svc = memory.NewService(nil, repo, memory.WithIndexer(failing))
	annotation, learned := "edit", "new lesson"
	if _, err := svc.Update(context.Background(), 0, "c2", memory.UpdatePatch{
		Annotation: &annotation,
		Learned:    &learned,
	}); err == nil {
		t.Fatal("expected reindex failure")
	}
	if repo.byID["c2"].Annotation != "" || repo.byID["c2"].QuestionSummary != "kept" ||
		repo.byID["c2"].Outcome.Learned != "old lesson" || len(repo.byID["c2"].Tags) != 1 || repo.byID["c2"].Tags[0] != "old" {
		t.Fatalf("failed update must restore original: %+v", repo.byID["c2"])
	}
}

func TestMemoryService_SearchHidesUnownedFromAuthenticated(t *testing.T) {
	repo := &searchMemRepo{results: []*entity.CaseMemoryProjection{{CaseID: "c-own"}, {CaseID: "c-open"}}}
	cases := &memCaseRepo{byID: map[string]*entity.DecisionCase{
		"c-own":  {ID: "c-own", UserID: 7},
		"c-open": {ID: "c-open", UserID: 0}, // unowned legacy case
	}}
	svc := memory.NewService(nil, repo, memory.WithCaseRepo(cases))

	// authenticated user (userID 7): own case visible, unowned hidden
	out, err := svc.Search(context.Background(), 7, "q", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(out) != 1 || out[0].CaseID != "c-own" {
		t.Fatalf("authenticated search leaked unowned: %+v", out)
	}

	// open mode (userID 0): everything visible (unchanged)
	outOpen, err := svc.Search(context.Background(), 0, "q", 10)
	if err != nil {
		t.Fatalf("search open: %v", err)
	}
	if len(outOpen) != 2 {
		t.Fatalf("open mode should see all: %+v", outOpen)
	}
}
