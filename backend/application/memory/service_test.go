package memory_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/domain/entity"
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
