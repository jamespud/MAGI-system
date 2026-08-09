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

func openLockDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.SchedulerLockModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSchedulerLock_ExcludesSecondOwner(t *testing.T) {
	lock := magi.NewSchedulerLock(openLockDB(t))
	ctx := context.Background()
	ok, err := lock.Acquire(ctx, "recurring-tick", "a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	ok, err = lock.Acquire(ctx, "recurring-tick", "b", time.Minute)
	if err != nil || ok {
		t.Fatalf("second owner must be excluded: ok=%v err=%v", ok, err)
	}
	if err := lock.Release(ctx, "recurring-tick", "a"); err != nil {
		t.Fatalf("release: %v", err)
	}
	ok, err = lock.Acquire(ctx, "recurring-tick", "b", time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire after release: ok=%v err=%v", ok, err)
	}
}

func TestSchedulerLock_ExpiresLease(t *testing.T) {
	lock := magi.NewSchedulerLock(openLockDB(t))
	ctx := context.Background()
	ok, err := lock.Acquire(ctx, "tick", "a", 30*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("acquire: ok=%v err=%v", ok, err)
	}
	time.Sleep(60 * time.Millisecond)
	ok, err = lock.Acquire(ctx, "tick", "b", time.Minute)
	if err != nil || !ok {
		t.Fatalf("expired lease must be stealable: ok=%v err=%v", ok, err)
	}
}
