package decision_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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

type remoteCancelOrchestrator struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (o *remoteCancelOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	close(o.started)
	<-ctx.Done()
	close(o.cancelled)
	return nil, context.Cause(ctx)
}

func TestRunManager_RemoteCancelStopsWorkerAndFencesLateTerminalWrite(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-remote-cancel"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	lease := 30 * time.Millisecond
	orch := &remoteCancelOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rmB := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", LeaseDuration: lease, MaxAttempts: 1,
	})
	if err := rmB.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start replica B: %v", err)
	}
	defer rmB.Cancel(caseID)
	<-orch.started

	job, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if err := jobs.Cancel(context.Background(), job.ID); err != nil {
		t.Fatalf("replica A cancel job: %v", err)
	}
	if err := repo.CaseRepo().UpdateStatus(context.Background(), caseID, entity.CaseStatusCancelled); err != nil {
		t.Fatalf("replica A cancel case: %v", err)
	}

	select {
	case <-orch.cancelled:
	case <-time.After(5 * lease):
		t.Fatal("remote worker did not stop after lease/status loss")
	}

	writer, ok := repo.CaseRepo().(port.ConditionalCaseStatusWriter)
	if !ok {
		t.Fatal("production case repository must provide conditional status writes")
	}
	updated, err := writer.UpdateStatusIfCurrent(context.Background(), caseID,
		[]entity.CaseStatus{entity.CaseStatusEvaluating}, entity.CaseStatusResolved)
	if err != nil {
		t.Fatalf("late terminal write: %v", err)
	}
	if updated {
		t.Fatal("late terminal write must not overwrite CANCELLED")
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusCancelled {
		t.Fatalf("case after late write = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil || jobAfter.Status != entity.DecisionJobCancelled {
		t.Fatalf("job after late write = %+v err=%v", jobAfter, err)
	}
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
