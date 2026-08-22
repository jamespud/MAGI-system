package rag

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubRagIndexJobRepo struct {
	jobs []*entity.RagIndexJob
}

func (s *stubRagIndexJobRepo) Enqueue(_ context.Context, kind entity.RagIndexJobKind, source, sourceRef string, maxAttempts int) (*entity.RagIndexJob, error) {
	job := &entity.RagIndexJob{ID: "ri-1", Kind: kind, Source: source, SourceRef: sourceRef, MaxAttempts: maxAttempts, Status: entity.RagIndexJobQueued}
	s.jobs = append(s.jobs, job)
	return job, nil
}
func (s *stubRagIndexJobRepo) Claim(context.Context, string, string, time.Time) (*entity.RagIndexJob, bool, error) {
	return nil, false, nil
}
func (s *stubRagIndexJobRepo) Heartbeat(context.Context, string, string, time.Time) error { return nil }
func (s *stubRagIndexJobRepo) MarkSucceeded(context.Context, string, string) error       { return nil }
func (s *stubRagIndexJobRepo) MarkFailed(context.Context, string, string, string, *time.Time) error {
	return nil
}
func (s *stubRagIndexJobRepo) Cancel(context.Context, string) error                       { return nil }
func (s *stubRagIndexJobRepo) RequeueExpired(context.Context, time.Time) error            { return nil }
func (s *stubRagIndexJobRepo) ListRunnable(context.Context, time.Time) ([]*entity.RagIndexJob, error) {
	return nil, nil
}

func TestDurableIndexer_StoreEnqueuesCaseMemoryIndex(t *testing.T) {
	repo := &stubRagIndexJobRepo{}
	d := NewDurableIndexer(nil, repo)
	if _, err := d.Store(context.Background(), &entity.CaseMemoryProjection{CaseID: "case-1"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(repo.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(repo.jobs))
	}
	j := repo.jobs[0]
	if j.Kind != entity.RagIndexJobKindIndex || j.Source != port.SourceCaseMemory || j.SourceRef != "case-1" {
		t.Errorf("job = %+v, want index/case_memory/case-1", j)
	}
}

func TestDurableIndexer_StoreDocumentEnqueuesKnowledgeIndex(t *testing.T) {
	repo := &stubRagIndexJobRepo{}
	d := NewDurableIndexer(nil, repo)
	if _, err := d.StoreDocument(context.Background(), &entity.KnowledgeDoc{ID: "kd-1"}); err != nil {
		t.Fatalf("store document: %v", err)
	}
	j := repo.jobs[0]
	if j.Kind != entity.RagIndexJobKindIndex || j.Source != port.SourceKnowledgeDoc || j.SourceRef != "kd-1" {
		t.Errorf("job = %+v, want index/knowledge_doc/kd-1", j)
	}
}

func TestDurableIndexer_DeleteSourceEnqueuesDelete(t *testing.T) {
	repo := &stubRagIndexJobRepo{}
	d := NewDurableIndexer(nil, repo)
	if err := d.DeleteSource(context.Background(), port.SourceCaseMemory, "case-2"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	j := repo.jobs[0]
	if j.Kind != entity.RagIndexJobKindDelete || j.Source != port.SourceCaseMemory || j.SourceRef != "case-2" {
		t.Errorf("job = %+v, want delete/case_memory/case-2", j)
	}
}

func TestDurableIndexer_ImplementsAllPorts(t *testing.T) {
	var _ port.KnowledgePort = (*DurableIndexer)(nil)
	var _ port.MemoryIndexer = (*DurableIndexer)(nil)
	var _ port.DocumentIndexer = (*DurableIndexer)(nil)
}
