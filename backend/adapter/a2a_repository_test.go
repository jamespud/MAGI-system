package magi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	a2a "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newA2ASubmissionRepo(t *testing.T) (*gorm.DB, a2a.SubmissionRepository) {
	t.Helper()
	db := openConversationDB(t)
	return db, magi.NewA2ASubmissionRepository(db)
}

func a2aPrepareCommand(userID int64, messageID, hash, taskID, contextID string) a2a.PrepareCommand {
	return a2a.PrepareCommand{
		SubmissionID: "sub-" + taskID, MessageID: messageID, RequestHash: hash,
		TaskID: taskID, ContextID: contextID, InputMessageID: "input-" + taskID,
		CaseMessageID: "case-msg-" + taskID, UserID: userID,
		Question: "Should MAGI expose A2A?", Background: "interoperate",
		Constraints:     []entity.Constraint{{Key: "security", Value: "required", Hard: true}},
		MaxDebateRounds: 3,
	}
}

func TestA2ASubmissionPrepare_ReplaysEqualHash(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	cmd := a2aPrepareCommand(7, "message-1", "same", "task-1", "context-1")

	first, created, err := repo.Prepare(ctx, cmd)
	if err != nil || !created {
		t.Fatalf("first prepare = (%+v, %v, %v)", first, created, err)
	}
	second, created, err := repo.Prepare(ctx, cmd)
	if err != nil || created {
		t.Fatalf("replay = (%+v, %v, %v)", second, created, err)
	}
	if second.Binding.ID != first.Binding.ID || second.Case.ID != first.Case.ID {
		t.Fatalf("replay created a different binding: first=%+v second=%+v", first.Binding, second.Binding)
	}
	assertA2ASubmissionCounts(t, db, 1, 1, 1, 2)
}

func TestA2ASubmissionPrepare_RejectsDifferentHashForSameOwnerMessage(t *testing.T) {
	_, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "first", "task-1", "context-1")); err != nil {
		t.Fatal(err)
	}
	_, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "second", "task-2", "context-2"))
	if !errors.Is(err, a2a.ErrIdempotencyConflict) {
		t.Fatalf("different hash error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestA2ASubmissionPrepare_AllowsSameMessageForDifferentOwners(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	for _, userID := range []int64{7, 8} {
		if _, created, err := repo.Prepare(ctx, a2aPrepareCommand(userID, "shared-message", "hash", fmt.Sprintf("task-%d", userID), fmt.Sprintf("context-%d", userID))); err != nil || !created {
			t.Fatalf("prepare user %d = created %v, err %v", userID, created, err)
		}
	}
	assertA2ASubmissionCounts(t, db, 2, 2, 2, 4)
}

func TestA2ASubmissionPrepare_RejectsForeignContext(t *testing.T) {
	_, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1")); err != nil {
		t.Fatal(err)
	}
	_, _, err := repo.Prepare(ctx, a2aPrepareCommand(8, "message-2", "hash", "task-2", "context-1"))
	if !errors.Is(err, a2a.ErrForbidden) {
		t.Fatalf("foreign context error = %v, want ErrForbidden", err)
	}
}

func TestA2ASubmissionPrepare_HydratesExistingContextHistory(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if err := db.Create(&magi.ConversationModel{ID: "context-1", UserID: 7, Title: "prior"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.ConversationMessageModel{ID: "prior-user", ConversationID: "context-1", UserID: 7, Role: entity.ConversationRoleUser, Content: "What is the risk?"}).Error; err != nil {
		t.Fatal(err)
	}
	consensus, err := json.Marshal(entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.ResolutionModel{ID: "resolution-1", CaseID: "case-prior", FinalDecision: string(entity.VoteDecisionApprove), ConsensusJSON: string(consensus)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.ConversationMessageModel{ID: "prior-case", ConversationID: "context-1", UserID: 7, Role: entity.ConversationRoleAssistant, CaseID: "case-prior"}).Error; err != nil {
		t.Fatal(err)
	}

	prepared, created, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1"))
	if err != nil || !created {
		t.Fatalf("prepare = (%+v, %v, %v)", prepared, created, err)
	}
	if !strings.Contains(prepared.Case.Context, "What is the risk?") || !strings.Contains(prepared.Case.Context, "Previous decision (case-prior)") {
		t.Fatalf("case context did not hydrate transaction history: %q", prepared.Case.Context)
	}
}

func TestA2ASubmissionTaskAccess_IsOwnerScoped(t *testing.T) {
	_, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByTask(ctx, 8, "task-1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("foreign get error = %v, want not found", err)
	}
	page, err := repo.ListTasks(ctx, a2a.TaskListFilter{UserID: 8, Limit: 10})
	if err != nil || len(page.Records) != 0 {
		t.Fatalf("foreign list = (%+v, %v), want no records", page, err)
	}
	result, err := repo.CancelTask(ctx, 8, "task-1")
	if err != nil || result == nil || result.Record != nil || result.Outcome != a2a.CancelNotFound {
		t.Fatalf("foreign cancel = (%+v, %v), want not found", result, err)
	}
}

// TestA2ASubmissionCancel_RejectedBindingIsNotCancelable guards the invariant
// that a REJECTED binding is terminal from the A2A client's perspective even
// though the Case stays DRAFT: CancelTask must report not-cancelable and must
// not flip the Case to CANCELLED or emit an event.
func TestA2ASubmissionCancel_RejectedBindingIsNotCancelable(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1")); err != nil {
		t.Fatal(err)
	}
	token := "reject-token"
	if _, claimed, err := repo.ClaimStart(ctx, "sub-task-1", token, time.Now().Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("claim for rejection = claimed %v err %v", claimed, err)
	}
	if err := repo.SettleRejected(ctx, "sub-task-1", token, "budget_exceeded"); err != nil {
		t.Fatal(err)
	}
	result, err := repo.CancelTask(ctx, 7, "task-1")
	if err != nil || result == nil || result.Outcome != a2a.CancelNotCancelable || result.Event != nil {
		t.Fatalf("rejected cancel = %+v err=%v, want not-cancelable without event", result, err)
	}
	var caseModel magi.CaseModel
	if err := db.Where("id = ?", "task-1").First(&caseModel).Error; err != nil {
		t.Fatal(err)
	}
	if caseModel.Status != string(entity.CaseStatusDraft) {
		t.Fatalf("case status = %s, want DRAFT (rejected cancel must not touch the case)", caseModel.Status)
	}
	var events int64
	if err := db.Model(&magi.EventModel{}).Where("case_id = ?", "task-1").Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("rejected cancel wrote %d durable events, want 0", events)
	}
}

func TestA2ASubmissionPrepare_ConcurrentReplayCreatesOneAtomicSnapshot(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	cmd := a2aPrepareCommand(7, "message-1", "same", "task-1", "context-1")
	const callers = 100
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared, _, err := repo.Prepare(ctx, cmd)
			if err == nil && (prepared == nil || prepared.Case == nil || prepared.Conversation == nil) {
				err = errors.New("prepared snapshot was incomplete")
			}
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertA2ASubmissionCounts(t, db, 1, 1, 1, 2)
}

func TestA2ASubmissionPrepare_RetriesConversationInsertConflictsUntilContextExists(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	var collisions atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("a2a_test_conversation_insert_conflict", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*magi.ConversationModel); ok && collisions.Add(1) <= 3 {
			tx.AddError(errors.New("UNIQUE constraint failed: magi_conversation.id"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("a2a_test_conversation_insert_conflict") })

	prepared, created, err := repo.Prepare(context.Background(), a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1"))
	if err != nil || !created || prepared == nil || prepared.Conversation == nil {
		t.Fatalf("prepare after conversation conflicts = (%+v, %v, %v)", prepared, created, err)
	}
	if got := collisions.Load(); got != 4 {
		t.Fatalf("conversation insert attempts = %d, want 4", got)
	}
	assertA2ASubmissionCounts(t, db, 1, 1, 1, 2)
}

func TestA2ASubmissionPrepare_StopsAfterConversationContentionBudget(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	var collisions atomic.Int32
	if err := db.Callback().Create().Before("gorm:create").Register("a2a_test_persistent_conversation_insert_conflict", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*magi.ConversationModel); ok {
			collisions.Add(1)
			tx.AddError(errors.New("UNIQUE constraint failed: magi_conversation.id"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove("a2a_test_persistent_conversation_insert_conflict") })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := repo.Prepare(ctx, a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1"))
	if !errors.Is(err, a2a.ErrContextContention) {
		t.Fatalf("persistent conversation contention error = %v, want ErrContextContention", err)
	}
	if got := collisions.Load(); got < 2 {
		t.Fatalf("conversation insert attempts = %d, want bounded retries", got)
	}
	assertA2ASubmissionCounts(t, db, 0, 0, 0, 0)
}

func TestA2ASubmissionPrepare_ConcurrentMessagesShareOneNewContext(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	const callers = 20
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, created, err := repo.Prepare(context.Background(), a2aPrepareCommand(7,
				fmt.Sprintf("message-%d", i), "hash", fmt.Sprintf("task-%d", i), "context-1"))
			if err == nil && !created {
				err = fmt.Errorf("message-%d unexpectedly replayed", i)
			}
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertA2ASubmissionCounts(t, db, callers, callers, 1, callers*2)
}

func seedA2AListTask(t *testing.T, db *gorm.DB, repo a2a.SubmissionRepository, userID int64, messageID, taskID, contextID string, createdAt time.Time) {
	t.Helper()
	if _, _, err := repo.Prepare(context.Background(), a2aPrepareCommand(userID, messageID, "hash", taskID, contextID)); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).Where("task_id = ?", taskID).
		Updates(map[string]any{"created_at": createdAt, "updated_at": createdAt}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestA2ASubmissionListTasks_KeysetPagesAcrossOwners(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	at := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	// Tied timestamps across two owners: keyset must be deterministic and
	// never leak another owner's rows.
	for i, taskID := range []string{"case-1", "case-2", "case-3"} {
		seedA2AListTask(t, db, repo, 7, fmt.Sprintf("m-%d", i), taskID, "ctx-1", at)
	}
	seedA2AListTask(t, db, repo, 8, "m-other", "case-other", "ctx-other", at)

	var all []string
	var cursor *a2a.TaskCursor
	for page := 0; page < 3; page++ {
		pageResult, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 2, After: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if pageResult.Total != 3 {
			t.Fatalf("total = %d, want 3", pageResult.Total)
		}
		for _, rec := range pageResult.Records {
			if rec.Submission.UserID != 7 {
				t.Fatalf("cross-owner record leaked: %+v", rec.Submission)
			}
			all = append(all, rec.Submission.TaskID)
		}
		cursor = pageResult.Next
		if cursor == nil {
			break
		}
	}
	if !reflect.DeepEqual(all, []string{"case-3", "case-2", "case-1"}) {
		t.Fatalf("paged order = %v", all)
	}
}

func TestA2ASubmissionListTasks_StatusFilterMatchesProjector(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	seedA2AListTask(t, db, repo, 7, "m-1", "case-1", "ctx-1", time.Now().Add(-3*time.Hour))
	seedA2AListTask(t, db, repo, 7, "m-2", "case-2", "ctx-1", time.Now().Add(-2*time.Hour))
	seedA2AListTask(t, db, repo, 7, "m-3", "case-3", "ctx-1", time.Now().Add(-1*time.Hour))

	// case-1 working (INVESTIGATING + running job), case-2 completed, case-3 canceled.
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusInvestigating)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.DecisionJobModel{ID: "job-1", CaseID: "case-1", Status: string(entity.DecisionJobRunning), AvailableAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-2").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-3").Update("status", string(entity.CaseStatusCancelled)).Error; err != nil {
		t.Fatal(err)
	}

	working, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, Status: "TASK_STATE_WORKING", IncludeArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(working.Records) != 1 || working.Records[0].Submission.TaskID != "case-1" {
		t.Fatalf("working records = %+v", working.Records)
	}
	completed, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, Status: "TASK_STATE_COMPLETED"})
	if err != nil || len(completed.Records) != 1 || completed.Records[0].Submission.TaskID != "case-2" {
		t.Fatalf("completed records = %+v err=%v", completed.Records, err)
	}
	canceled, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, Status: "TASK_STATE_CANCELED"})
	if err != nil || len(canceled.Records) != 1 || canceled.Records[0].Submission.TaskID != "case-3" {
		t.Fatalf("canceled records = %+v err=%v", canceled.Records, err)
	}
}

func TestA2ASubmissionListTasks_StatusTimestampAfter(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	seedA2AListTask(t, db, repo, 7, "m-1", "case-1", "ctx-1", time.Now().Add(-3*time.Hour))
	seedA2AListTask(t, db, repo, 7, "m-2", "case-2", "ctx-1", time.Now().Add(-1*time.Hour))
	cutoff := time.Now().Add(-2 * time.Hour)
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("updated_at", time.Now().Add(-3*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-2").Update("updated_at", time.Now().Add(-30*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, StatusTimestampAfter: &cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].Submission.TaskID != "case-2" {
		t.Fatalf("recent records = %+v err=%v", page.Records, err)
	}
}

func TestA2ASubmissionListTasks_IncludeArtifactsFalseSkipsResultTables(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	seedA2AListTask(t, db, repo, 7, "m-1", "case-1", "ctx-1", time.Now().Add(-1*time.Hour))
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	consensus, _ := json.Marshal(entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1})
	if err := db.Create(&magi.ResolutionModel{ID: "res-1", CaseID: "case-1", FinalDecision: string(entity.VoteDecisionApprove), ConsensusJSON: string(consensus)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.VoteModel{ID: "vote-1", CaseID: "case-1", Decision: string(entity.VoteDecisionApprove), Confidence: 80}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.EvidenceModel{ID: "EV-1", CaseID: "case-1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.ClaimModel{ID: "CL-1", CaseID: "case-1"}).Error; err != nil {
		t.Fatal(err)
	}

	light, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, IncludeArtifacts: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(light.Records) != 1 {
		t.Fatalf("light records = %d", len(light.Records))
	}
	rec := light.Records[0]
	if rec.Resolution != nil || len(rec.Evidence) != 0 || len(rec.Claims) != 0 || len(rec.Votes) != 0 {
		t.Fatalf("result tables loaded despite IncludeArtifacts=false: %+v", rec)
	}
	if rec.Case == nil || rec.Submission.TaskID != "case-1" {
		t.Fatalf("core record missing: %+v", rec)
	}

	full, err := repo.ListTasks(context.Background(), a2a.TaskListFilter{UserID: 7, Limit: 10, IncludeArtifacts: true})
	if err != nil {
		t.Fatal(err)
	}
	rec = full.Records[0]
	if rec.Resolution == nil || len(rec.Evidence) != 1 || len(rec.Claims) != 1 || len(rec.Votes) != 1 {
		t.Fatalf("result tables missing with IncludeArtifacts=true: %+v", rec)
	}
}

func a2aTerminalCaseStatus(s entity.CaseStatus) bool {
	switch s {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

// TestA2ASnapshot_ConsistentCaseAndEventSeq guards the snapshot invariant that
// GetTaskRecord must not combine a pre-terminal Case with a terminal
// MaxEventSeq. The read is paused after the Case query while a terminal
// transaction commits; resuming must never yield a working Case paired with a
// terminal event watermark, because the stream uses that watermark to skip
// catch-up and would permanently miss the terminal event.
func TestA2ASnapshot_ConsistentCaseAndEventSeq(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "a2a_snapshot.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	dbA, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlA, _ := dbA.DB()
	sqlA.SetMaxOpenConns(1)
	if err := dbA.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatal(err)
	}
	repo := magi.NewA2ASubmissionRepository(dbA)
	if _, _, err := repo.Prepare(context.Background(), a2aPrepareCommand(7, "message-1", "hash", "task-1", "context-1")); err != nil {
		t.Fatal(err)
	}
	if err := dbA.Model(&magi.CaseModel{}).Where("id = ?", "task-1").Update("status", string(entity.CaseStatusInvestigating)).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbA.Create(&magi.DecisionJobModel{ID: "job-1", CaseID: "task-1", Status: string(entity.DecisionJobRunning), AvailableAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	resolutionQueried := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	if err := dbA.Callback().Query().After("gorm:query").Register("a2a_test_snapshot_barrier", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*magi.ResolutionModel); ok {
			once.Do(func() { close(resolutionQueried) })
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbA.Callback().Query().Remove("a2a_test_snapshot_barrier") })

	type readResult struct {
		rec *a2a.TaskRecord
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		rec, err := repo.GetTaskRecord(context.Background(), 7, "task-1")
		done <- readResult{rec: rec, err: err}
	}()
	<-resolutionQueried

	// A terminal transaction commits while the snapshot read is paused after
	// the Case query: the case status, job, resolution, and terminal event all
	// land together on the other connection.
	dbB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlB, _ := dbB.DB()
	sqlB.SetMaxOpenConns(1)
	defer sqlB.Close()

	consensus, err := json.Marshal(entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1})
	if err != nil {
		t.Fatal(err)
	}
	txErr := dbB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&magi.CaseModel{}).Where("id = ?", "task-1").
			Updates(map[string]any{"status": string(entity.CaseStatusResolved), "updated_at": time.Now().UTC()}).Error; err != nil {
			return err
		}
		if err := tx.Model(&magi.DecisionJobModel{}).Where("case_id = ?", "task-1").Update("status", string(entity.DecisionJobSucceeded)).Error; err != nil {
			return err
		}
		if err := tx.Create(&magi.ResolutionModel{ID: "res-1", CaseID: "task-1", FinalDecision: string(entity.VoteDecisionApprove), ConsensusJSON: string(consensus)}).Error; err != nil {
			return err
		}
		return tx.Create(&magi.EventModel{ID: "ev-1", CaseID: "task-1", Seq: 1, Type: string(entity.EventCaseCompleted), Timestamp: time.Now().UTC()}).Error
	})
	if txErr != nil {
		t.Fatalf("terminal commit: %v", txErr)
	}
	close(release)

	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.rec == nil {
		t.Fatal("nil record")
	}
	if res.rec.MaxEventSeq > 0 && !a2aTerminalCaseStatus(res.rec.Case.Status) {
		t.Fatalf("snapshot combined pre-terminal case %s with terminal event seq %d", res.rec.Case.Status, res.rec.MaxEventSeq)
	}
}

func assertA2ASubmissionCounts(t *testing.T, db *gorm.DB, submissions, cases, conversations, messages int64) {
	t.Helper()
	for _, check := range []struct {
		model any
		want  int64
	}{
		{&magi.A2ASubmissionModel{}, submissions},
		{&magi.CaseModel{}, cases},
		{&magi.ConversationModel{}, conversations},
		{&magi.ConversationMessageModel{}, messages},
	} {
		var got int64
		if err := db.Model(check.model).Count(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%T count = %d, want %d", check.model, got, check.want)
		}
	}
}

// TestA2ASubmission_OutputModesRoundTrip proves negotiated modes survive
// process reconstruction: Prepare persists them, and GetTaskRecord reads them
// back in stable order so a later artifact projection honors the contract.
func TestA2ASubmission_OutputModesRoundTrip(t *testing.T) {
	_, repo := newA2ASubmissionRepo(t)
	cmd := a2aPrepareCommand(7, "msg-roundtrip", "hash-roundtrip", "case-roundtrip", "conv-roundtrip")
	cmd.AcceptedOutputModes = []string{"application/json", "text/markdown"}
	if _, created, err := repo.Prepare(context.Background(), cmd); err != nil || !created {
		t.Fatalf("prepare = created %v err %v", created, err)
	}
	rec, err := repo.GetTaskRecord(context.Background(), 7, "case-roundtrip")
	if err != nil {
		t.Fatalf("get task record: %v", err)
	}
	if !reflect.DeepEqual(rec.Submission.AcceptedOutputModes, []string{"text/markdown", "application/json"}) {
		t.Fatalf("roundtrip modes = %v", rec.Submission.AcceptedOutputModes)
	}
	if len(rec.Submission.AcceptedOutputModes) != 2 {
		t.Fatalf("accepted modes len = %d", len(rec.Submission.AcceptedOutputModes))
	}
}

// TestA2ASubmission_FailsClosedOnMalformedOutputModes proves a corrupted
// accepted_output_modes_json column is surfaced as an error instead of being
// silently replaced with both default modes, which would broaden the output a
// client never negotiated.
func TestA2ASubmission_FailsClosedOnMalformedOutputModes(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	cmd := a2aPrepareCommand(7, "msg-malformed", "hash-malformed", "case-malformed", "conv-malformed")
	if _, created, err := repo.Prepare(context.Background(), cmd); err != nil || !created {
		t.Fatalf("prepare = created %v err %v", created, err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).
		Where("task_id = ?", "case-malformed").
		Update("accepted_output_modes_json", "{not-json").Error; err != nil {
		t.Fatalf("corrupt stored modes: %v", err)
	}
	if _, err := repo.GetTaskRecord(context.Background(), 7, "case-malformed"); err == nil {
		t.Fatal("expected fail-closed error for malformed stored output modes")
	}
}

// TestA2ASubmission_FailsClosedOnNullOutputModes guards the fail-closed
// contract against a stored literal null: json.Unmarshal maps null to a nil
// slice, which must not be treated as an empty list that broadens output.
func TestA2ASubmission_FailsClosedOnNullOutputModes(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	cmd := a2aPrepareCommand(7, "msg-null", "hash-null", "case-null", "conv-null")
	if _, created, err := repo.Prepare(context.Background(), cmd); err != nil || !created {
		t.Fatalf("prepare = created %v err %v", created, err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).
		Where("task_id = ?", "case-null").
		Update("accepted_output_modes_json", "null").Error; err != nil {
		t.Fatalf("corrupt stored modes: %v", err)
	}
	if _, err := repo.GetTaskRecord(context.Background(), 7, "case-null"); err == nil {
		t.Fatal("expected fail-closed error for null stored output modes")
	}
}

// TestA2ASubmission_FailsClosedOnUnsupportedOutputMode proves an out-of-band
// stored mode that the write path never normalizes is refused instead of
// silently exposing unnegotiated output to the client.
func TestA2ASubmission_FailsClosedOnUnsupportedOutputMode(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	cmd := a2aPrepareCommand(7, "msg-unsupported", "hash-unsupported", "case-unsupported", "conv-unsupported")
	if _, created, err := repo.Prepare(context.Background(), cmd); err != nil || !created {
		t.Fatalf("prepare = created %v err %v", created, err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).
		Where("task_id = ?", "case-unsupported").
		Update("accepted_output_modes_json", `["text/html"]`).Error; err != nil {
		t.Fatalf("corrupt stored modes: %v", err)
	}
	if _, err := repo.GetTaskRecord(context.Background(), 7, "case-unsupported"); err == nil {
		t.Fatal("expected fail-closed error for unsupported stored output modes")
	}
}

// TestA2ASubmission_EmptyOutputModesNormalizeToDefaults pins the distinction
// between an explicit empty array (allowed, default semantics) and null
// (rejected): the write path already normalizes an empty list.
func TestA2ASubmission_EmptyOutputModesNormalizeToDefaults(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	cmd := a2aPrepareCommand(7, "msg-empty", "hash-empty", "case-empty", "conv-empty")
	if _, created, err := repo.Prepare(context.Background(), cmd); err != nil || !created {
		t.Fatalf("prepare = created %v err %v", created, err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).
		Where("task_id = ?", "case-empty").
		Update("accepted_output_modes_json", "[]").Error; err != nil {
		t.Fatalf("corrupt stored modes: %v", err)
	}
	rec, err := repo.GetTaskRecord(context.Background(), 7, "case-empty")
	if err != nil {
		t.Fatalf("get task record: %v", err)
	}
	if !reflect.DeepEqual(rec.Submission.AcceptedOutputModes, []string{"text/markdown", "application/json"}) {
		t.Fatalf("empty modes = %v, want defaults", rec.Submission.AcceptedOutputModes)
	}
}

// TestA2ASubmission_ListClaimableSkipsCorruptBinding proves recovery does not
// abort on the first unloadable binding: the corrupt row is quarantined to
// REJECTED and the healthy later binding is still returned.
func TestA2ASubmission_ListClaimableSkipsCorruptBinding(t *testing.T) {
	db, repo := newA2ASubmissionRepo(t)
	ctx := context.Background()
	if _, created, err := repo.Prepare(ctx, a2aPrepareCommand(7, "msg-early", "hash-early", "case-early", "conv-early")); err != nil || !created {
		t.Fatalf("prepare early = created %v err %v", created, err)
	}
	if err := db.Model(&magi.A2ASubmissionModel{}).
		Where("task_id = ?", "case-early").
		Update("accepted_output_modes_json", "null").Error; err != nil {
		t.Fatalf("corrupt early binding: %v", err)
	}
	if _, created, err := repo.Prepare(ctx, a2aPrepareCommand(7, "msg-late", "hash-late", "case-late", "conv-late")); err != nil || !created {
		t.Fatalf("prepare late = created %v err %v", created, err)
	}
	batch, err := repo.ListClaimable(ctx, 100, "")
	if err != nil {
		t.Fatalf("list claimable must not abort on a corrupt binding: %v", err)
	}
	if len(batch) != 1 || batch[0].Binding.TaskID != "case-late" {
		t.Fatalf("list claimable = %+v, want only the healthy later binding", batch)
	}
	var model magi.A2ASubmissionModel
	if err := db.Where("task_id = ?", "case-early").First(&model).Error; err != nil {
		t.Fatalf("load early binding: %v", err)
	}
	if model.State != string(a2a.SubmissionRejected) {
		t.Fatalf("corrupt binding state = %s, want REJECTED", model.State)
	}
}
