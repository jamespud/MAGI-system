package bootstrap

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

type gateBarrierKey struct{}

func waitGateBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("gate barrier timed out")
	}
}
func waitGateDBLock(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waits int64
		err := db.Raw(`SELECT COUNT(*) FROM performance_schema.data_lock_waits w
   JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID
   WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='decision_writer_contract'`).Scan(&waits).Error
		if err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("no database-observed gate lock wait")
}

// Both directions are controlled by barriers at the actual shared row lock;
// no timing assumption is used as evidence of linearization.
func TestMySQLDecisionWriter_GateLinearizesAdmitAndClaim(t *testing.T) {
	for _, operation := range []string{"Admit", "Claim"} {
		for _, first := range []string{"block", "execution"} {
			t.Run(operation+"_"+first+"_wins", func(t *testing.T) {
				_, db := t6SchemaFixture(t)
				repo := magi.NewRepository(db)
				c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
				if err := repo.CaseRepo().Create(context.Background(), c); err != nil {
					t.Fatal(err)
				}
				jobs := magi.NewDecisionJobRepository(db)
				var job *entity.DecisionJob
				if operation == "Claim" {
					var err error
					job, _, err = jobs.Admit(context.Background(), c.ID, 3, 0)
					if err != nil {
						t.Fatal(err)
					}
				}
				reached, release := make(chan struct{}), make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				position := "Before"
				if first == "execution" {
					position = "After"
				}
				barrier := func(tx *gorm.DB) {
					if tx.Statement.Table != "decision_writer_contract" || tx.Statement.Context.Value(gateBarrierKey{}) == nil {
						return
					}
					close(reached)
					select {
					case <-release:
					case <-tx.Statement.Context.Done():
					}
				}
				name := "t6_gate_" + operation + first
				if position == "Before" {
					db.Callback().Query().Before("gorm:query").Register(name, barrier)
				} else {
					db.Callback().Query().After("gorm:query").Register(name, barrier)
				}
				defer db.Callback().Query().Remove(name)
				ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), gateBarrierKey{}, true), 15*time.Second)
				defer cancel()
				execution := make(chan error, 1)
				go func() {
					if operation == "Admit" {
						_, _, err := jobs.Admit(ctx, c.ID, 3, 0)
						execution <- err
					} else {
						_, _, err := jobs.Claim(ctx, job.ID, "w", uuid.NewString(), time.Now().Add(time.Minute))
						execution <- err
					}
				}()
				waitGateBarrier(t, reached)
				closed := make(chan error, 1)
				if first == "block" {
					_, err := BlockDecisionWriter(context.Background(), db)
					if err != nil {
						t.Fatal(err)
					}
					close(release)
					if err := <-execution; !errors.Is(err, port.ErrDecisionWriterBlocked) {
						t.Fatalf("execution passed committed close: %v", err)
					}
				} else {
					go func() { _, err := BlockDecisionWriter(context.Background(), db); closed <- err }()
					waitGateDBLock(t, db)
					close(release)
					if err := <-execution; err != nil {
						t.Fatalf("shared lock holder denied: %v", err)
					}
					if err := <-closed; err != nil {
						t.Fatal(err)
					}
				}
				fresh, err := repo.CaseRepo().Get(context.Background(), c.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantGeneration := int64(0)
				if first == "execution" && operation == "Claim" {
					wantGeneration = 1
				}
				if fresh.ExecutionGeneration != wantGeneration {
					t.Fatalf("generation %d expected %d", fresh.ExecutionGeneration, wantGeneration)
				}
				var n int64
				db.Model(&magi.DecisionJobClaimModel{}).Where("case_id=?", c.ID).Count(&n)
				if n != wantGeneration {
					t.Fatalf("claim receipts=%d", n)
				}
				if _, _, err := jobs.Admit(context.Background(), c.ID, 3, 0); !errors.Is(err, port.ErrDecisionWriterBlocked) {
					t.Fatalf("admission reopened: %v", err)
				}
				if operation == "Claim" {
					if _, _, err := jobs.Claim(context.Background(), job.ID, "w", uuid.NewString(), time.Now().Add(time.Minute)); !errors.Is(err, port.ErrDecisionWriterBlocked) {
						t.Fatalf("claim reopened: %v", err)
					}
				}
				for _, model := range []any{&magi.ResolutionModel{}, &magi.EventModel{}} {
					db.Model(model).Where("case_id=?", c.ID).Count(&n)
					if n != 0 {
						t.Fatal("gate race created terminal effects")
					}
				}
			})
		}
	}
}
