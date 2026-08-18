package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openConversationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestConversationRepository_RoundTripAndOwnership(t *testing.T) {
	db := openConversationDB(t)
	repo := magi.NewConversationRepository(db)
	ctx := context.Background()

	convs := []*entity.Conversation{
		{ID: "conv-a", UserID: 7, Title: "Alpha"},
		{ID: "conv-b", UserID: 8, Title: "Beta"},
		{ID: "conv-c", UserID: 7, Title: "Gamma"},
	}
	for _, conv := range convs {
		if err := repo.Create(ctx, conv); err != nil {
			t.Fatalf("create conversation: %v", err)
		}
	}

	got, err := repo.Get(ctx, "conv-a")
	if err != nil || got == nil || got.UserID != 7 || got.Title != "Alpha" {
		t.Fatalf("get conversation: %v %+v", err, got)
	}

	listed, err := repo.ListByUser(ctx, 7, 10, 0)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list owner conversations: %v len=%d", err, len(listed))
	}
	for _, conv := range listed {
		if conv.UserID != 7 {
			t.Fatalf("cross-tenant conversation leaked: %+v", conv)
		}
	}

	at := time.Now()
	messages := []*entity.ConversationMessage{
		{ID: "msg-1", ConversationID: "conv-a", UserID: 7, Role: entity.ConversationRoleUser, Content: "first", CreatedAt: at},
		{ID: "msg-2", ConversationID: "conv-a", UserID: 7, Role: entity.ConversationRoleAssistant, Content: "second", CaseID: "case-1", CreatedAt: at},
		{ID: "msg-other", ConversationID: "conv-b", UserID: 8, Role: entity.ConversationRoleUser, Content: "other", CreatedAt: at},
	}
	for _, msg := range messages {
		if err := repo.AppendMessage(ctx, msg); err != nil {
			t.Fatalf("append message: %v", err)
		}
	}

	stored, err := repo.ListMessages(ctx, "conv-a", 10)
	if err != nil || len(stored) != 2 {
		t.Fatalf("list messages: %v len=%d", err, len(stored))
	}
	if stored[0].ID != "msg-1" || stored[1].ID != "msg-2" || stored[1].CaseID != "case-1" {
		t.Fatalf("message order/content wrong: %+v", stored)
	}

	updated, err := repo.Get(ctx, "conv-a")
	if err != nil || !updated.UpdatedAt.After(at) {
		t.Fatalf("conversation updated_at was not bumped: %v %+v", err, updated)
	}
}

func TestConversationRepository_DeleteCascadesMessages(t *testing.T) {
	db := openConversationDB(t)
	repo := magi.NewConversationRepository(db)
	ctx := context.Background()
	conv := &entity.Conversation{ID: "conv-delete", UserID: 9, Title: "Delete"}
	if err := repo.Create(ctx, conv); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.AppendMessage(ctx, &entity.ConversationMessage{ID: "msg-delete", ConversationID: conv.ID, UserID: 9, Role: entity.ConversationRoleUser, Content: "gone"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := repo.Delete(ctx, conv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, conv.ID); err == nil {
		t.Fatal("conversation still exists after delete")
	}
	msgs, err := repo.ListMessages(ctx, conv.ID, 10)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("messages not cascaded: %v len=%d", err, len(msgs))
	}
}
