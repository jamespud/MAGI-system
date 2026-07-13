package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/service"
)

type stubEventRepo struct {
	events map[string][]*entity.MagiEvent
}

func (s *stubEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error {
	s.events[e.CaseID] = append(s.events[e.CaseID], e)
	return nil
}
func (s *stubEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return s.events[caseID], nil
}

var _ port.EventRepository = (*stubEventRepo)(nil)

func TestReplay_SortedByTimestamp(t *testing.T) {
	repo := &stubEventRepo{events: make(map[string][]*entity.MagiEvent)}
	base := time.Now()
	repo.events["c1"] = []*entity.MagiEvent{
		{ID: "e3", CaseID: "c1", Type: entity.EventCaseCompleted, Timestamp: base.Add(3 * time.Second)},
		{ID: "e1", CaseID: "c1", Type: entity.EventCaseCreated, Timestamp: base},
		{ID: "e2", CaseID: "c1", Type: entity.EventAgentStarted, Timestamp: base.Add(1 * time.Second)},
	}
	events, err := service.Replay(context.Background(), "c1", repo)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].ID != "e1" || events[1].ID != "e2" || events[2].ID != "e3" {
		t.Fatalf("order: %s %s %s", events[0].ID, events[1].ID, events[2].ID)
	}
}
