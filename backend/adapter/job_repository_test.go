package magi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDecisionJobRepository_LifecycleAndRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.DecisionJobModel{}, &magi.CaseModel{}, &magi.RunAdmissionLockModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewDecisionJobRepository(db)
	if err := db.Create(&magi.CaseModel{ID: "case-1"}).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	job, admitted, err := repo.Admit(context.Background(), "case-1", 2, 0)
	if err != nil || job.Status != entity.DecisionJobQueued {
		t.Fatalf("admit: job=%+v admitted=%v err=%v", job, admitted, err)
	}
	if !admitted {
		t.Fatal("first admit should be admitted")
	}
	lease := time.Now().Add(time.Minute)
	claimed, ok, err := repo.Claim(context.Background(), job.ID, "worker-1", lease)
	if err != nil || !ok || claimed.Attempt != 1 || claimed.Status != entity.DecisionJobRunning {
		t.Fatalf("claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := repo.Heartbeat(context.Background(), job.ID, "worker-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := repo.MarkFailed(context.Background(), job.ID, "worker-1", "transient", &retryAt); err != nil {
		t.Fatalf("mark retry: %v", err)
	}
	runnable, err := repo.ListRunnable(context.Background(), time.Now())
	if err != nil || len(runnable) != 1 || runnable[0].LastError != "transient" {
		t.Fatalf("runnable retry: jobs=%+v err=%v", runnable, err)
	}
	claimed, ok, err = repo.Claim(context.Background(), job.ID, "worker-2", lease)
	if err != nil || !ok || claimed.Attempt != 2 {
		t.Fatalf("second claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := repo.MarkSucceeded(context.Background(), job.ID, "worker-2"); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	final, err := repo.GetByCase(context.Background(), "case-1")
	if err != nil || final.Status != entity.DecisionJobSucceeded {
		t.Fatalf("final: job=%+v err=%v", final, err)
	}
}

func TestDecisionJobRepository_OwnerMutationsReportLeaseLoss(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.DecisionJobModel{}, &magi.CaseModel{}, &magi.RunAdmissionLockModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewDecisionJobRepository(db)
	if err := db.Create(&magi.CaseModel{ID: "case-lease-loss"}).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	job, _, err := repo.Admit(context.Background(), "case-lease-loss", 1, 0)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok, err := repo.Claim(context.Background(), job.ID, "worker-a", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := repo.Cancel(context.Background(), job.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	for name, mutate := range map[string]func() error{
		"heartbeat": func() error {
			return repo.Heartbeat(context.Background(), job.ID, "worker-a", time.Now().Add(time.Minute))
		},
		"succeeded": func() error { return repo.MarkSucceeded(context.Background(), job.ID, "worker-a") },
		"failed":    func() error { return repo.MarkFailed(context.Background(), job.ID, "worker-a", "late", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("error = %v, want ErrLeaseLost", err)
			}
		})
	}
}

func openAdmissionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite admission: %v", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.DecisionJobModel{}, &magi.RunAdmissionLockModel{}); err != nil {
		t.Fatalf("migrate admission: %v", err)
	}
	return db
}

func TestDecisionJobRepo_AdmitWithinLimitIsAtomic(t *testing.T) {
	db := openAdmissionDB(t)
	repo := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	if err := db.Create(&magi.CaseModel{ID: "case-admit-1", UserID: 7, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case1: %v", err)
	}
	job, admitted, err := repo.Admit(ctx, "case-admit-1", 3, 1)
	if err != nil || !admitted {
		t.Fatalf("first admit = job=%+v admitted=%v err=%v", job, admitted, err)
	}
	if job.Status != entity.DecisionJobQueued {
		t.Fatalf("first admit status = %s, want queued", job.Status)
	}
	if err := db.Create(&magi.CaseModel{ID: "case-admit-2", UserID: 7, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case2: %v", err)
	}
	job2, admitted2, err := repo.Admit(ctx, "case-admit-2", 3, 1)
	if err != nil {
		t.Fatalf("second admit err = %v", err)
	}
	if admitted2 || job2 != nil {
		t.Fatalf("second admit should be limited: job=%+v admitted=%v", job2, admitted2)
	}
}

func TestDecisionJobRepo_AdmitExistingActiveJobIsIdempotent(t *testing.T) {
	db := openAdmissionDB(t)
	repo := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	if err := db.Create(&magi.CaseModel{ID: "case-existing", UserID: 9, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	first, admitted, err := repo.Admit(ctx, "case-existing", 3, 8)
	if err != nil || !admitted {
		t.Fatalf("first admit = job=%+v admitted=%v err=%v", first, admitted, err)
	}
	second, admitted2, err := repo.Admit(ctx, "case-existing", 5, 8)
	if err != nil || !admitted2 {
		t.Fatalf("idempotent admit = job=%+v admitted=%v err=%v", second, admitted2, err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("idempotent admit returned a different job: first=%+v second=%+v", first, second)
	}
	if second.Status != entity.DecisionJobQueued {
		t.Fatalf("idempotent admit status = %s, want queued", second.Status)
	}
}

func TestDecisionJobRepo_TerminalJobDoesNotConsumeCapacity(t *testing.T) {
	db := openAdmissionDB(t)
	repo := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	if err := db.Create(&magi.CaseModel{ID: "case-term-1", UserID: 11, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case1: %v", err)
	}
	job, _, err := repo.Admit(ctx, "case-term-1", 3, 1)
	if err != nil {
		t.Fatalf("admit case1: %v", err)
	}
	// Directly transition the job to a terminal state without claiming it.
	if err := db.Model(&magi.DecisionJobModel{}).Where("id = ?", job.ID).
		Updates(map[string]any{"status": string(entity.DecisionJobFailed)}).Error; err != nil {
		t.Fatalf("force-fail job: %v", err)
	}
	if err := db.Create(&magi.CaseModel{ID: "case-term-2", UserID: 11, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case2: %v", err)
	}
	job2, admitted2, err := repo.Admit(ctx, "case-term-2", 3, 1)
	if err != nil || !admitted2 {
		t.Fatalf("terminal job must not consume capacity: job=%+v admitted=%v err=%v", job2, admitted2, err)
	}
	if job2.Status != entity.DecisionJobQueued {
		t.Fatalf("terminal-ignoring admit status = %s, want queued", job2.Status)
	}
}

func TestTerminalCommitter_DoesNotWriteArtifactsAfterCancellation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.ResolutionModel{}, &magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	committer, ok := repo.(port.TerminalCommitter)
	if !ok {
		t.Fatal("production repository must provide terminal commit fencing")
	}
	caseID := "case-terminal-cancelled"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusCancelled}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	event := entity.NewEvent(caseID, "", nil, entity.EventCaseCompleted, map[string]any{"status": string(entity.CaseStatusResolved)})
	committed, err := committer.CommitTerminal(context.Background(), caseID, entity.CaseStatusResolved, entity.CaseStatusResolved,
		&entity.Resolution{ID: "res-terminal-cancelled", CaseID: caseID, FinalDecision: entity.VoteDecisionApprove}, &event)
	if err != nil {
		t.Fatalf("commit terminal: %v", err)
	}
	if committed {
		t.Fatal("terminal commit must lose to a prior cancellation")
	}
	if _, err := repo.ResolutionRepo().Get(context.Background(), caseID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("resolution after rejected terminal commit: %v", err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), caseID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after rejected terminal commit: %+v err=%v", events, err)
	}
}

func TestMagiRepository_CommitStatusTransitionCommitsCaseAndEvent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	committer, ok := repo.(port.StatusTransitionCommitter)
	if !ok {
		t.Fatal("production repository must provide atomic status transition commits")
	}
	caseID := "case-status-committed"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusInvestigating}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	event := entity.NewEvent(caseID, "", nil, entity.EventCaseStatusChanged,
		map[string]any{"status": string(entity.CaseStatusEvidenceGating), "round": 1})
	committed, err := committer.CommitStatusTransition(context.Background(), caseID,
		[]entity.CaseStatus{entity.CaseStatusInvestigating}, entity.CaseStatusEvidenceGating, &event)
	if err != nil || !committed {
		t.Fatalf("commit status transition: committed=%v err=%v", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusEvidenceGating {
		t.Fatalf("case after transition = %+v err=%v", caseAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), caseID)
	if err != nil || len(events) != 1 || events[0].Type != entity.EventCaseStatusChanged ||
		events[0].Seq != 1 || event.Seq != 1 {
		t.Fatalf("transition events = %+v err=%v event=%+v", events, err, event)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", caseID).Error; err != nil || cursor.NextSeq != 2 {
		t.Fatalf("transition cursor = %+v err=%v", cursor, err)
	}
}

func TestMagiRepository_CommitStatusTransitionFenceLossWritesNothing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	committer, ok := repo.(port.StatusTransitionCommitter)
	if !ok {
		t.Fatal("production repository must provide atomic status transition commits")
	}
	caseID := "case-status-fence"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusInvestigating}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	event := entity.NewEvent(caseID, "", nil, entity.EventCaseStatusChanged,
		map[string]any{"status": string(entity.CaseStatusEvidenceGating), "round": 1})
	committed, err := committer.CommitStatusTransition(context.Background(), caseID,
		[]entity.CaseStatus{entity.CaseStatusResolved}, entity.CaseStatusEvidenceGating, &event)
	if err != nil || committed {
		t.Fatalf("fence loss = committed=%v err=%v, want no commit", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusInvestigating {
		t.Fatalf("case after fence loss = %+v err=%v", caseAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), caseID)
	if err != nil || len(events) != 0 {
		t.Fatalf("events after fence loss = %+v err=%v", events, err)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", caseID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cursor after fence loss = %+v err=%v, want no cursor", cursor, err)
	}
}

func TestMagiRepository_CommitStatusTransitionRollsBackOnEventFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	committer, ok := repo.(port.StatusTransitionCommitter)
	if !ok {
		t.Fatal("production repository must provide atomic status transition commits")
	}
	caseID := "case-status-rollback"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusInvestigating}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	duplicate := entity.NewEvent(caseID, "", nil, entity.EventCaseStatusChanged,
		map[string]any{"status": string(entity.CaseStatusInvestigating)})
	if err := repo.EventRepo().Create(context.Background(), &duplicate); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	event := entity.NewEvent(caseID, "", nil, entity.EventCaseStatusChanged,
		map[string]any{"status": string(entity.CaseStatusEvidenceGating), "round": 1})
	event.ID = duplicate.ID
	committed, err := committer.CommitStatusTransition(context.Background(), caseID,
		[]entity.CaseStatus{entity.CaseStatusInvestigating}, entity.CaseStatusEvidenceGating, &event)
	if err == nil || committed {
		t.Fatalf("duplicate event insert = committed=%v err=%v, want rollback", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusInvestigating {
		t.Fatalf("case after event rollback = %+v err=%v", caseAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), caseID)
	if err != nil || len(events) != 1 || events[0].ID != duplicate.ID || events[0].Seq != 1 {
		t.Fatalf("events after event rollback = %+v err=%v", events, err)
	}
	if event.Seq != 0 {
		t.Fatalf("event sequence after rollback = %d, want restored 0", event.Seq)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", caseID).Error; err != nil || cursor.NextSeq != 2 {
		t.Fatalf("cursor after event rollback = %+v err=%v", cursor, err)
	}
}

func newFinalFailureFixture(t *testing.T, caseID string) (*gorm.DB, port.Repository, port.DecisionJobRepository, *entity.DecisionJob) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusInvestigating}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	job, _, err := jobs.Admit(context.Background(), caseID, 1, 0)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	claimed, ok, err := jobs.Claim(context.Background(), job.ID, "worker-a", time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	return db, repo, jobs, claimed
}

func TestDecisionJobRepository_CommitFinalFailureAtomically(t *testing.T) {
	db, repo, jobs, job := newFinalFailureFixture(t, "case-final-failure")
	event := entity.NewEvent("case-final-failure", "", nil, entity.EventCaseFailed, map[string]any{"status": "FAILED"})
	committed, err := jobs.CommitFinalFailure(context.Background(), job.ID, "worker-a", "case-final-failure",
		[]entity.CaseStatus{entity.CaseStatusInvestigating}, "boom", &event)
	if err != nil || !committed {
		t.Fatalf("commit final failure: committed=%v err=%v", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), "case-final-failure")
	if err != nil || caseAfter.Status != entity.CaseStatusFailed {
		t.Fatalf("case after final failure = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), "case-final-failure")
	if err != nil || jobAfter.Status != entity.DecisionJobFailed || jobAfter.LastError != "boom" {
		t.Fatalf("job after final failure = %+v err=%v", jobAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), "case-final-failure")
	if err != nil || len(events) != 1 || events[0].Type != entity.EventCaseFailed || events[0].Seq != 1 || event.Seq != 1 {
		t.Fatalf("failure events = %+v err=%v event=%+v", events, err, event)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", "case-final-failure").Error; err != nil || cursor.NextSeq != 2 {
		t.Fatalf("failure cursor = %+v err=%v", cursor, err)
	}
}

func TestDecisionJobRepository_CommitFinalFailureFenceLossRollsBack(t *testing.T) {
	db, repo, jobs, job := newFinalFailureFixture(t, "case-final-fence")
	event := entity.NewEvent("case-final-fence", "", nil, entity.EventCaseFailed, map[string]any{"status": "FAILED"})
	committed, err := jobs.CommitFinalFailure(context.Background(), job.ID, "worker-b", "case-final-fence",
		[]entity.CaseStatus{entity.CaseStatusInvestigating}, "late", &event)
	if err != nil || committed {
		t.Fatalf("fence loss = committed=%v err=%v, want no commit", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), "case-final-fence")
	if err != nil || caseAfter.Status != entity.CaseStatusInvestigating {
		t.Fatalf("case after fence loss = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), "case-final-fence")
	if err != nil || jobAfter.Status != entity.DecisionJobRunning || jobAfter.WorkerID != "worker-a" {
		t.Fatalf("job after fence loss = %+v err=%v", jobAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), "case-final-fence")
	if err != nil || len(events) != 0 {
		t.Fatalf("events after fence loss = %+v err=%v", events, err)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", "case-final-fence").Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cursor after fence loss = %+v err=%v, want no cursor", cursor, err)
	}
}

func TestDecisionJobRepository_CommitFinalFailureEventInsertRollback(t *testing.T) {
	db, repo, jobs, job := newFinalFailureFixture(t, "case-final-event-rollback")
	duplicate := entity.NewEvent("case-final-event-rollback", "", nil, entity.EventCaseStatusChanged, map[string]any{"status": "INVESTIGATING"})
	if err := repo.EventRepo().Create(context.Background(), &duplicate); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	failed := entity.NewEvent("case-final-event-rollback", "", nil, entity.EventCaseFailed, map[string]any{"status": "FAILED"})
	failed.ID = duplicate.ID
	committed, err := jobs.CommitFinalFailure(context.Background(), job.ID, "worker-a", "case-final-event-rollback",
		[]entity.CaseStatus{entity.CaseStatusInvestigating}, "boom", &failed)
	if err == nil || committed {
		t.Fatalf("duplicate event insert = committed=%v err=%v, want rollback", committed, err)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), "case-final-event-rollback")
	if err != nil || caseAfter.Status != entity.CaseStatusInvestigating {
		t.Fatalf("case after event rollback = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), "case-final-event-rollback")
	if err != nil || jobAfter.Status != entity.DecisionJobRunning || jobAfter.WorkerID != "worker-a" {
		t.Fatalf("job after event rollback = %+v err=%v", jobAfter, err)
	}
	events, err := repo.EventRepo().ListByCase(context.Background(), "case-final-event-rollback")
	if err != nil || len(events) != 1 || events[0].ID != duplicate.ID || events[0].Seq != 1 {
		t.Fatalf("events after event rollback = %+v err=%v", events, err)
	}
	var cursor magi.EventCursorModel
	if err := db.First(&cursor, "case_id = ?", "case-final-event-rollback").Error; err != nil || cursor.NextSeq != 2 {
		t.Fatalf("cursor after event rollback = %+v err=%v", cursor, err)
	}
}

func TestDecisionJobRepository_CommitFinalFailureRejectsPublicTerminalCase(t *testing.T) {
	terminalStatuses := []entity.CaseStatus{
		entity.CaseStatusResolved,
		entity.CaseStatusMemoryIndexed,
		entity.CaseStatusFailed,
		entity.CaseStatusCancelled,
		entity.CaseStatusTimedOut,
		entity.CaseStatusInsufficientEv,
		entity.CaseStatusDeadlocked,
	}
	for _, status := range terminalStatuses {
		t.Run(string(status), func(t *testing.T) {
			caseID := "case-final-terminal-" + string(status)
			db, repo, jobs, job := newFinalFailureFixture(t, caseID)
			if err := db.Model(&magi.CaseModel{}).Where("id = ?", caseID).Update("status", string(status)).Error; err != nil {
				t.Fatal(err)
			}
			event := entity.NewEvent(caseID, "", nil, entity.EventCaseFailed, map[string]any{"status": "FAILED"})
			committed, err := jobs.CommitFinalFailure(context.Background(), job.ID, "worker-a", caseID,
				[]entity.CaseStatus{status}, "late", &event)
			if err != nil || committed {
				t.Fatalf("terminal case commit = committed=%v err=%v, want false,nil", committed, err)
			}
			caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
			if err != nil || caseAfter.Status != status {
				t.Fatalf("case after rejected commit = %+v err=%v", caseAfter, err)
			}
			jobAfter, err := jobs.GetByCase(context.Background(), caseID)
			if err != nil || jobAfter.Status != entity.DecisionJobRunning {
				t.Fatalf("job after rejected commit = %+v err=%v", jobAfter, err)
			}
			events, err := repo.EventRepo().ListByCase(context.Background(), caseID)
			if err != nil || len(events) != 0 {
				t.Fatalf("events after rejected commit = %+v err=%v", events, err)
			}
		})
	}
}
