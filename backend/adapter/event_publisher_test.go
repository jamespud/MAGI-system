package magi_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type captureEventPublisher struct{ events []entity.MagiEvent }

func (p *captureEventPublisher) Publish(ctx context.Context, e entity.MagiEvent) error {
	p.events = append(p.events, e)
	return nil
}

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

func TestEventPublisher_FanoutAfterPersist(t *testing.T) {
	store := magi.NewInMemoryEventRepo()
	live := &captureEventPublisher{}
	pub := magi.NewEventPublisherAdapterWithFanout(store, live)
	if err := pub.Publish(context.Background(), entity.MagiEvent{ID: "e2", CaseID: "c2"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(live.events) != 1 || live.events[0].ID != "e2" {
		t.Fatalf("live events: %+v", live.events)
	}
}

func TestEventPublisher_NilStore(t *testing.T) {
	pub := magi.NewEventPublisherAdapter(nil)
	if err := pub.Publish(context.Background(), entity.MagiEvent{CaseID: "c1"}); err != nil {
		t.Fatalf("publish with nil store should not error: %v", err)
	}
}

func testSQLiteDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestCheckpointRepository_SaveLoad(t *testing.T) {
	db := testSQLiteDB(t)
	if err := db.AutoMigrate(&magi.CheckpointModel{}); err != nil {
		t.Fatalf("migrate checkpoint: %v", err)
	}
	repo := magi.NewRepository(db).CheckpointRepo()
	want := &entity.AgentState{
		RunID: "run-1", Messages: []entity.MessageRef{{Role: "user", Content: "question"}},
		MessagesJSON: `[{"role":"user","content":"question"}]`, StepCount: 3, TokenUsed: 17, Phase: "gather",
	}
	if err := repo.Save(context.Background(), want); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	want.StepCount = 4
	if err := repo.Save(context.Background(), want); err != nil {
		t.Fatalf("upsert checkpoint: %v", err)
	}
	got, err := repo.Load(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if got.StepCount != 4 || got.TokenUsed != 17 || len(got.Messages) != 1 || got.Messages[0].Content != "question" {
		t.Fatalf("checkpoint: %+v", got)
	}
}

func TestEventPublisher_RedactsSecretsBeforePersist(t *testing.T) {
	store := magi.NewInMemoryEventRepo()
	live := &captureEventPublisher{}
	pub := magi.NewEventPublisherAdapterWithRedaction(store, live, redact.New("sk-secret-1"))
	ev := entity.NewEvent("c1", "", nil, entity.EventToolCallRequested, map[string]any{"arguments": `{"api_key":"sk-secret-1"}`})
	if err := pub.Publish(context.Background(), ev); err != nil {
		t.Fatalf("publish: %v", err)
	}
	events, _ := store.ListByCase(context.Background(), "c1")
	if len(events) != 1 || string(events[0].Payload) == "" {
		t.Fatalf("events: %+v", events)
	}
	if got := string(events[0].Payload); strings.Contains(got, "sk-secret-1") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("payload not redacted: %s", got)
	}
}
