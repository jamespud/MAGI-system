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

func TestEventBroker_PublishAssignsPerCaseSequence(t *testing.T) {
	b := server.NewEventBroker()
	if err := b.Publish(context.Background(), entity.MagiEvent{ID: "c1-1", CaseID: "c1"}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := b.Publish(context.Background(), entity.MagiEvent{ID: "c1-2", CaseID: "c1"}); err != nil {
		t.Fatalf("publish second: %v", err)
	}

	events, err := b.ListByCase(context.Background(), "c1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("sequences: %+v", events)
	}
	after, err := b.ListAfterSeq(context.Background(), "c1", 1, 100)
	if err != nil {
		t.Fatalf("list after seq: %v", err)
	}
	if len(after) != 1 || after[0].Seq != 2 {
		t.Fatalf("after sequence: %+v", after)
	}
}

func TestEventBroker_MixedCreateAndPublishUsesMaxSequence(t *testing.T) {
	b := server.NewEventBroker()
	ctx := context.Background()
	if err := b.Create(ctx, &entity.MagiEvent{ID: "c1-7", CaseID: "c1", Seq: 7}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := b.Publish(ctx, entity.MagiEvent{ID: "c1-8", CaseID: "c1"}); err != nil {
		t.Fatalf("publish inferred: %v", err)
	}
	if err := b.Publish(ctx, entity.MagiEvent{ID: "c1-50", CaseID: "c1", Seq: 50}); err != nil {
		t.Fatalf("publish explicit: %v", err)
	}
	if err := b.Publish(ctx, entity.MagiEvent{ID: "c1-51", CaseID: "c1"}); err != nil {
		t.Fatalf("publish after explicit: %v", err)
	}

	events, err := b.ListByCase(ctx, "c1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	want := []uint64{7, 8, 50, 51}
	for i, event := range events {
		if event.Seq != want[i] {
			t.Fatalf("event %d sequence: got %d, want %d", i, event.Seq, want[i])
		}
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

func TestEventBroker_SubscribeWithReplay_ReturnsHistory(t *testing.T) {
	b := server.NewEventBroker()
	b.Publish(context.Background(), entity.MagiEvent{ID: "c1-1", CaseID: "c1", Type: entity.EventCaseCreated})
	b.Publish(context.Background(), entity.MagiEvent{ID: "c1-2", CaseID: "c1", Type: entity.EventAgentStarted})

	ch, history := b.SubscribeWithReplay("c1")
	defer b.Unsubscribe("c1", ch)

	if len(history) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(history))
	}
	if history[0].ID != "c1-1" || history[1].ID != "c1-2" {
		t.Fatalf("history order wrong: %s, %s", history[0].ID, history[1].ID)
	}
}

func TestEventBroker_SubscribeWithReplay_LiveEventAfterHistory(t *testing.T) {
	b := server.NewEventBroker()
	b.Publish(context.Background(), entity.MagiEvent{ID: "c1-1", CaseID: "c1", Type: entity.EventCaseCreated})
	ch, history := b.SubscribeWithReplay("c1")
	defer b.Unsubscribe("c1", ch)

	if len(history) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(history))
	}

	b.Publish(context.Background(), entity.MagiEvent{ID: "c1-2", CaseID: "c1", Type: entity.EventAgentStarted})

	select {
	case ev := <-ch:
		if ev.ID != "c1-2" {
			t.Fatalf("live event id: %s", ev.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for live event")
	}
}
