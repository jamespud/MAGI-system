package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openQuotaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.ToolQuotaCounterModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestToolQuotaRepository_EnforcesWindowLimit(t *testing.T) {
	repo := magi.NewToolQuotaRepository(openQuotaDB(t))
	ctx := context.Background()
	window := time.Now().Truncate(time.Minute)
	if ok, _ := repo.TryConsume(ctx, 7, "code_runner", window, 2); !ok {
		t.Fatal("first call should be allowed")
	}
	if ok, _ := repo.TryConsume(ctx, 7, "code_runner", window, 2); !ok {
		t.Fatal("second call should be allowed")
	}
	if ok, _ := repo.TryConsume(ctx, 7, "code_runner", window, 2); ok {
		t.Fatal("third call must be denied")
	}
	// A different user or a fresh window is allowed.
	if ok, _ := repo.TryConsume(ctx, 8, "code_runner", window, 2); !ok {
		t.Fatal("other user should be allowed")
	}
	if ok, _ := repo.TryConsume(ctx, 7, "code_runner", window.Add(time.Minute), 2); !ok {
		t.Fatal("fresh window should be allowed")
	}
}
