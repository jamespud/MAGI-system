package replay_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubEventRepo struct {
	events []*entity.MagiEvent
}

func (s *stubEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error { return nil }
func (s *stubEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return s.events, nil
}
func (s *stubEventRepo) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	return s.events, nil
}

func TestReplayService_Replay(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(time.Second)
	repo := &stubEventRepo{events: []*entity.MagiEvent{
		{CaseID: "c1", Type: entity.EventCaseCompleted, Timestamp: t2},
		{CaseID: "c1", Type: entity.EventCaseCreated, Timestamp: t1},
	}}
	svc := replay.NewService(repo)
	events, err := svc.Replay(context.Background(), "c1")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Timestamp.After(events[1].Timestamp) {
		t.Fatal("events not sorted by timestamp")
	}
}

func TestReplayService_Timeline(t *testing.T) {
	repo := &stubEventRepo{events: []*entity.MagiEvent{}}
	svc := replay.NewService(repo)
	events, err := svc.Timeline(context.Background(), "c1")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if events == nil {
		t.Fatal("expected non-nil events")
	}
}
