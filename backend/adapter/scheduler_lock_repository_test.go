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

// The owner must be able to renew its own lease, and every acquisition must
// leave a fresh updated_at: that field is the only freshness signal operators
// (and the MySQL branch, before this fix) can read.
func TestSchedulerLock_OwnerRenewsAndTimestampAdvances(t *testing.T) {
	db := openLockDB(t)
	lock := magi.NewSchedulerLock(db)
	ctx := context.Background()

	if ok, err := lock.Acquire(ctx, "tick", "replica-a", time.Minute); err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	var first magi.SchedulerLockModel
	if err := db.First(&first, "name = ?", "tick").Error; err != nil {
		t.Fatalf("read lock: %v", err)
	}

	time.Sleep(5 * time.Millisecond)
	if ok, err := lock.Acquire(ctx, "tick", "replica-a", time.Minute); err != nil || !ok {
		t.Fatalf("renew: ok=%v err=%v", ok, err)
	}
	var second magi.SchedulerLockModel
	if err := db.First(&second, "name = ?", "tick").Error; err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("renewal did not refresh updated_at: %v -> %v", first.UpdatedAt, second.UpdatedAt)
	}
	if !second.LeaseUntil.After(first.LeaseUntil) {
		t.Fatalf("renewal did not extend the lease: %v -> %v", first.LeaseUntil, second.LeaseUntil)
	}

	// A different replica must not steal a live lease.
	if ok, err := lock.Acquire(ctx, "tick", "replica-b", time.Minute); err != nil || ok {
		t.Fatalf("steal of a live lease = %v err=%v, want false", ok, err)
	}
}

func TestSchedulerLock_ExpiredLeaseIsTakenWithFreshTimestamp(t *testing.T) {
	db := openLockDB(t)
	lock := magi.NewSchedulerLock(db)
	ctx := context.Background()

	if err := db.Create(&magi.SchedulerLockModel{
		Name: "tick", Owner: "replica-a",
		LeaseUntil: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if ok, err := lock.Acquire(ctx, "tick", "replica-b", time.Minute); err != nil || !ok {
		t.Fatalf("takeover: ok=%v err=%v", ok, err)
	}
	var m magi.SchedulerLockModel
	if err := db.First(&m, "name = ?", "tick").Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.Owner != "replica-b" {
		t.Fatalf("owner = %q, want replica-b", m.Owner)
	}
	if time.Since(m.UpdatedAt) > 5*time.Second {
		t.Fatalf("takeover left a stale updated_at: %v", m.UpdatedAt)
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
