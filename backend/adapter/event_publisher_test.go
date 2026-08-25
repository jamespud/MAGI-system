package magi_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestEventPublisher_PersistsSequenceBeforeFanout(t *testing.T) {
	store := magi.NewInMemoryEventRepo()
	live := &captureEventPublisher{}
	pub := magi.NewEventPublisherAdapterWithFanout(store, live)
	if err := pub.Publish(context.Background(), entity.MagiEvent{ID: "e-seq", CaseID: "case-seq"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(live.events) != 1 || live.events[0].Seq != 1 {
		t.Fatalf("live event was not assigned sequence before fanout: %+v", live.events)
	}
}

func TestInMemoryEventRepo_MixedPublishAndCreateUsesMaxSequence(t *testing.T) {
	repo := magi.NewInMemoryEventRepo()
	pub := magi.NewEventPublisherAdapter(repo)
	ctx := context.Background()
	if err := pub.Publish(ctx, entity.MagiEvent{ID: "e-50", CaseID: "case-mixed", Seq: 50}); err != nil {
		t.Fatalf("publish explicit: %v", err)
	}
	if err := repo.Create(ctx, &entity.MagiEvent{ID: "e-51", CaseID: "case-mixed"}); err != nil {
		t.Fatalf("create inferred: %v", err)
	}
	events, err := repo.ListByCase(ctx, "case-mixed")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 || events[1].Seq != 51 {
		t.Fatalf("sequences: %+v", events)
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
	if err := db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
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

func TestEventRepository_ListAfterSeqConcurrent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db).EventRepo()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			caseID := "case-a"
			if i%2 == 1 {
				caseID = "case-b"
			}
			e := &entity.MagiEvent{ID: fmt.Sprintf("event-%03d", i), CaseID: caseID, Timestamp: time.Now()}
			if err := repo.Create(ctx, e); err != nil {
				t.Errorf("create %s: %v", caseID, err)
			}
		}(i)
	}
	wg.Wait()

	got, err := repo.ListAfterSeq(ctx, "case-a", 40, 100)
	if err != nil {
		t.Fatalf("list after seq: %v", err)
	}
	if len(got) != 60 {
		t.Fatalf("expected 60 events after seq 40, got %d", len(got))
	}
	for i, e := range got {
		want := uint64(i + 41)
		if e.Seq != want {
			t.Fatalf("event %d: got seq %d, want %d", i, e.Seq, want)
		}
	}
	if got[len(got)-1].Seq != 100 {
		t.Fatalf("last sequence: got %d, want 100", got[len(got)-1].Seq)
	}

	other, err := repo.ListByCase(ctx, "case-b")
	if err != nil {
		t.Fatalf("list other case: %v", err)
	}
	for i, e := range other {
		if e.Seq != uint64(i+1) {
			t.Fatalf("other case event %d: got seq %d, want %d", i, e.Seq, i+1)
		}
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
