package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
)

// The MySQL branch used to leave updated_at stale on takeover because
// ON DUPLICATE KEY UPDATE evaluates its assignments left to right. This test
// only runs against real MySQL (CI's mysql-migration job).
func TestSchedulerLock_MySQLTakeoverRefreshesTimestamp(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.SchedulerLockModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	lock := magi.NewSchedulerLock(db)
	ctx := context.Background()
	name := "tick-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { db.Exec("DELETE FROM magi_scheduler_lock WHERE name = ?", name) })

	if err := db.Create(&magi.SchedulerLockModel{
		Name: name, Owner: "replica-a",
		LeaseUntil: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if ok, err := lock.Acquire(ctx, name, "replica-b", time.Minute); err != nil || !ok {
		t.Fatalf("takeover: ok=%v err=%v", ok, err)
	}

	var m magi.SchedulerLockModel
	if err := db.First(&m, "name = ?", name).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.Owner != "replica-b" {
		t.Fatalf("owner = %q, want replica-b", m.Owner)
	}
	if age := time.Since(m.UpdatedAt); age > 10*time.Second {
		t.Fatalf("takeover left a stale updated_at (%s old): %v", age, m.UpdatedAt)
	}

	// A live lease owned by another replica is still not stealable.
	if ok, err := lock.Acquire(ctx, name, "replica-c", time.Minute); err != nil || ok {
		t.Fatalf("steal of a live lease = %v err=%v, want false", ok, err)
	}
}
