package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunCounter_AcquireRespectsLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.RunCounterModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRunCounterRepository(db)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		ok, err := repo.Acquire(ctx, 7, 2)
		if err != nil || !ok {
			t.Fatalf("acquire %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := repo.Acquire(ctx, 7, 2)
	if err != nil || ok {
		t.Fatalf("third acquire should be limited: ok=%v err=%v", ok, err)
	}
	if err := repo.Release(ctx, 7); err != nil {
		t.Fatalf("release: %v", err)
	}
	ok, err = repo.Acquire(ctx, 7, 2)
	if err != nil || !ok {
		t.Fatalf("acquire after release: ok=%v err=%v", ok, err)
	}
	// User IDs are independent.
	ok, err = repo.Acquire(ctx, 8, 2)
	if err != nil || !ok {
		t.Fatalf("other user acquire: ok=%v err=%v", ok, err)
	}
}
