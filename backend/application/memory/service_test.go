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
