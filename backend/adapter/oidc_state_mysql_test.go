package magi_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
)

// Two replicas racing on the same callback must not both consume the state.
func TestOIDCState_ConsumeIsSingleUseOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.OIDCStateModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repoA := magi.NewOIDCStateRepository(db)
	repoB := magi.NewOIDCStateRepository(db)
	ctx := context.Background()

	state := "state-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { db.Exec("DELETE FROM oidc_auth_state WHERE state = ?", state) })

	if err := repoA.Issue(ctx, state, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("issue: %v", err)
	}

	var wins atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		repo := repoA
		if i%2 == 1 {
			repo = repoB
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.Consume(ctx, state)
			if err != nil {
				t.Errorf("consume: %v", err)
				return
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("consume winners = %d, want exactly 1", got)
	}
	if ok, err := repoA.Consume(ctx, state); err != nil || ok {
		t.Fatalf("second consume = %v err=%v, want false", ok, err)
	}
}

// An expired state must not be consumable even if it was never used.
func TestOIDCState_ExpiredStateIsRejectedOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.OIDCStateModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewOIDCStateRepository(db)
	ctx := context.Background()

	state := "state-expired-" + time.Now().UTC().Format("20060102150405.000000000")
	t.Cleanup(func() { db.Exec("DELETE FROM oidc_auth_state WHERE state = ?", state) })

	if err := repo.Issue(ctx, state, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if ok, err := repo.Consume(ctx, state); err != nil || ok {
		t.Fatalf("expired consume = %v err=%v, want false", ok, err)
	}
}
