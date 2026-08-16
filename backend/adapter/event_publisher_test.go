package magi_test

import (
	"context"
	"strings"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
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

func TestEventRepository_ListAfterFiltersAndOrders(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.EventModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db).EventRepo()
	base := time.Now()
	events := []*entity.MagiEvent{
		{ID: "e1", CaseID: "c1", Type: entity.EventCaseCreated, Timestamp: base},
		{ID: "e2", CaseID: "c1", Type: entity.EventAgentStarted, Timestamp: base.Add(time.Second)},
		{ID: "e3", CaseID: "c1", Type: entity.EventVoteSubmitted, Timestamp: base.Add(2 * time.Second)},
		{ID: "other", CaseID: "c2", Type: entity.EventCaseCreated, Timestamp: base.Add(time.Second)},
	}
	for _, e := range events {
		if err := repo.Create(context.Background(), e); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// after = e1's timestamp (inclusive) -> e2, e3 (c2 excluded).
	after := base
	got, err := repo.ListAfter(context.Background(), "c1", after)
	if err != nil {
		t.Fatalf("listafter: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(got), got)
	}
	if got[0].ID != "e1" || got[1].ID != "e2" || got[2].ID != "e3" {
		t.Fatalf("ordering wrong: %+v", got)
	}

	// after = e2's timestamp -> only e3 for c1.
	got, err = repo.ListAfter(context.Background(), "c1", base.Add(time.Second))
	if err != nil {
		t.Fatalf("listafter2: %v", err)
	}
	if len(got) != 2 || got[0].ID != "e2" || got[1].ID != "e3" {
		t.Fatalf("inclusive boundary wrong: %+v", got)
	}

	// after = far future -> nothing.
	got, err = repo.ListAfter(context.Background(), "c1", base.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("listafter3: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestEventBroker_ListAfter(t *testing.T) {
	b := server.NewEventBroker()
	base := time.Now()
	_ = b.Publish(context.Background(), entity.MagiEvent{ID: "b1", CaseID: "c1", Timestamp: base})
	_ = b.Publish(context.Background(), entity.MagiEvent{ID: "b2", CaseID: "c1", Timestamp: base.Add(time.Second)})

	got, err := b.ListAfter(context.Background(), "c1", base.Add(time.Second))
	if err != nil {
		t.Fatalf("listafter: %v", err)
	}
	if len(got) != 1 || got[0].ID != "b2" {
		t.Fatalf("expected [b2], got %+v", got)
	}
}
