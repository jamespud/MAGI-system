package ragindex

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubInner struct {
	mu          sync.Mutex
	storeCalls  []string
	storeDocErr error
}

func (s *stubInner) Store(_ context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeCalls = append(s.storeCalls, proj.CaseID)
	return port.StoreStats{}, nil
}
func (s *stubInner) StoreDocument(_ context.Context, doc *entity.KnowledgeDoc) (port.StoreStats, error) {
	if s.storeDocErr != nil {
		return port.StoreStats{}, s.storeDocErr
	}
	return port.StoreStats{Chunks300: 5}, nil
}
func (s *stubInner) DeleteSource(_ context.Context, source, sourceRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeCalls = append(s.storeCalls, "delete:"+sourceRef)
	return nil
}

type stubJobRepo struct {
	mu    sync.Mutex
	claim map[string]int
}

func (s *stubJobRepo) Enqueue(_ context.Context, kind entity.RagIndexJobKind, source, sourceRef string, maxAttempts int) (*entity.RagIndexJob, error) {
	return &entity.RagIndexJob{ID: "ri-1", Kind: kind, Source: source, SourceRef: sourceRef, Status: entity.RagIndexJobQueued, MaxAttempts: maxAttempts}, nil
}
func (s *stubJobRepo) Claim(_ context.Context, jobID, workerID string, lease time.Time) (*entity.RagIndexJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claim == nil {
		s.claim = map[string]int{}
	}
	if s.claim[jobID] >= 1 {
		return nil, false, nil
	}
	s.claim[jobID]++
	return &entity.RagIndexJob{ID: jobID, Kind: entity.RagIndexJobKindIndex, Source: port.SourceCaseMemory, SourceRef: "case-1", Status: entity.RagIndexJobRunning, Attempt: 1, MaxAttempts: 3, WorkerID: workerID}, true, nil
}
func (s *stubJobRepo) Heartbeat(context.Context, string, string, time.Time) error { return nil }
func (s *stubJobRepo) MarkSucceeded(context.Context, string, string) error       { return nil }
func (s *stubJobRepo) MarkFailed(context.Context, string, string, string, *time.Time) error {
	return nil
}
func (s *stubJobRepo) Cancel(context.Context, string) error            { return nil }
func (s *stubJobRepo) RequeueExpired(context.Context, time.Time) error { return nil }
func (s *stubJobRepo) ListRunnable(context.Context, time.Time) ([]*entity.RagIndexJob, error) {
	return []*entity.RagIndexJob{{ID: "ri-1", Kind: entity.RagIndexJobKindIndex, Source: port.SourceCaseMemory, SourceRef: "case-1", Status: entity.RagIndexJobQueued, MaxAttempts: 3}}, nil
}

type stubMemRepo struct{}

func (s *stubMemRepo) Get(_ context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	return &entity.CaseMemoryProjection{CaseID: caseID}, nil
}
func (s *stubMemRepo) Save(context.Context, *entity.CaseMemoryProjection) error          { return nil }
func (s *stubMemRepo) Search(context.Context, string, int) ([]*entity.CaseMemoryProjection, error) {
	return nil, nil
}
func (s *stubMemRepo) List(context.Context) ([]*entity.CaseMemoryProjection, error) { return nil, nil }
func (s *stubMemRepo) Delete(context.Context, string) error                         { return nil }

type stubKnowRepo struct{}

func (s *stubKnowRepo) Create(context.Context, *entity.KnowledgeDoc) error { return nil }
func (s *stubKnowRepo) Get(_ context.Context, id string) (*entity.KnowledgeDoc, error) {
	return &entity.KnowledgeDoc{ID: id, Title: "t", Content: "c"}, nil
}
func (s *stubKnowRepo) ListByUser(context.Context, int64, int, int) ([]*entity.KnowledgeDoc, error) {
	return nil, nil
}
func (s *stubKnowRepo) Update(context.Context, *entity.KnowledgeDoc) error { return nil }
func (s *stubKnowRepo) Delete(context.Context, string) error               { return nil }
func (s *stubKnowRepo) ListAll(context.Context) ([]*entity.KnowledgeDoc, error) { return nil, nil }

func TestRagIndexPoller_ProcessesJob(t *testing.T) {
	inner := &stubInner{}
	p := NewRagIndexPoller(&stubJobRepo{}, &stubMemRepo{}, &stubKnowRepo{}, inner,
		PollerConfig{Interval: 10 * time.Millisecond, Lease: time.Second, WorkerID: "w1"})
	p.processJob(context.Background(), &entity.RagIndexJob{
		ID: "ri-1", Kind: entity.RagIndexJobKindIndex, Source: port.SourceCaseMemory,
		SourceRef: "case-1", Status: entity.RagIndexJobQueued, MaxAttempts: 3,
	})
	inner.mu.Lock()
	defer inner.mu.Unlock()
	if len(inner.storeCalls) != 1 || inner.storeCalls[0] != "case-1" {
		t.Errorf("storeCalls = %v, want [case-1]", inner.storeCalls)
	}
}
