package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
)

func TestEventBroker_PublishAndSubscribe(t *testing.T) {
	b := server.NewEventBroker()
	ch := b.Subscribe("c1")
	defer b.Unsubscribe("c1", ch)

	ev := &entity.MagiEvent{CaseID: "c1", Type: entity.EventVoteSubmitted}
	b.Publish(context.Background(), *ev)

	select {
	case got := <-ch:
		if got.Type != entity.EventVoteSubmitted {
			t.Fatalf("type: %s", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBroker_ListByCase(t *testing.T) {
	b := server.NewEventBroker()
	b.Publish(context.Background(), entity.MagiEvent{CaseID: "c1", Type: entity.EventCaseCreated})
	b.Publish(context.Background(), entity.MagiEvent{CaseID: "c1", Type: entity.EventCaseCompleted})

	events, err := b.ListByCase(context.Background(), "c1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestEventBroker_Unsubscribe(t *testing.T) {
	b := server.NewEventBroker()
	ch := b.Subscribe("c1")
	b.Unsubscribe("c1", ch)

	b.Publish(context.Background(), entity.MagiEvent{CaseID: "c1", Type: entity.EventCaseCreated})

	_, ok := <-ch
	if ok {
		t.Fatal("channel should be closed after unsubscribe")
	}
}

func TestEventBroker_NoSubscriberNonBlocking(t *testing.T) {
	b := server.NewEventBroker()
	err := b.Publish(context.Background(), entity.MagiEvent{CaseID: "c1", Type: entity.EventCaseCreated})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
}
