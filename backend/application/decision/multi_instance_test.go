package decision_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMultiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.DecisionJobModel{}, &magi.CaseModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestRunManager_DBLimitAcrossInstances(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 1, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	orch := &blockingUserOrchestrator{started: make(chan struct{}), release: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-a", MaxAttempts: 1, RetryBase: time.Millisecond,
		MaxConcurrentRunsPerUser: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 1}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	<-orch.started
	count, err := jobs.CountActiveByUser(context.Background(), 1)
	if err != nil || count != 1 {
		t.Fatalf("active count: %d err=%v", count, err)
	}
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "c2", UserID: 1, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case 2: %v", err)
	}
	// A second replica sees the same shared state and must reject.
	rm2 := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", MaxAttempts: 1, RetryBase: time.Millisecond,
		MaxConcurrentRunsPerUser: 1,
	})
	if err := rm2.Start(context.Background(), &entity.DecisionCase{ID: "c2", UserID: 1}); err == nil || err.Error() != decision.ErrRateLimited.Error() {
		t.Fatalf("second instance start: %v", err)
	}
	close(orch.release)
	rm.Cancel("c1")
}
