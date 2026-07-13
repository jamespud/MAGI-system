package magi_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestEventPublisher_StoreAndList(t *testing.T) {
	repo := magi.NewInMemoryEventRepo()
	pub := magi.NewEventPublisherAdapter(repo)
	e := entity.MagiEvent{ID: "e1", CaseID: "c1", Type: entity.EventCaseCreated}
	if err := pub.Publish(context.Background(), e); err != nil {
		t.Fatalf("publish: %v", err)
	}
	events, _ := repo.ListByCase(context.Background(), "c1")
	if len(events) != 1 || events[0].ID != "e1" {
		t.Fatalf("events: %+v", events)
	}
}

func TestEventPublisher_NilStore(t *testing.T) {
	pub := magi.NewEventPublisherAdapter(nil)
	if err := pub.Publish(context.Background(), entity.MagiEvent{CaseID: "c1"}); err != nil {
		t.Fatalf("publish with nil store should not error: %v", err)
	}
}
