package magi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	magi "github.com/jamespud/magi/backend/adapter"
	a2a "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func openA2AMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("MAGI_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration tests — a green run without it does NOT mean MySQL was verified")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("mysql unavailable: %v", err)
	}
	return db
}

// TestA2ASubmission_ConcurrentSameMessageOnMySQL proves the (user_id,
// message_id) unique key and the decision_job.case_id unique key serialize 100
// concurrent same-message submissions into exactly one Case and Job.
func TestA2ASubmission_ConcurrentSameMessageOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(
		&magi.A2ASubmissionModel{}, &magi.CaseModel{}, &magi.ConversationModel{},
		&magi.ConversationMessageModel{}, &magi.DecisionJobModel{},
	); err != nil {
		t.Fatal(err)
	}
	repo := magi.NewA2ASubmissionRepository(db)
	messageID := fmt.Sprintf("mysql-concurrent-%d", os.Getpid())
	_ = db.Exec("DELETE FROM a2a_submission WHERE message_id = ?", messageID)
	_ = db.Exec("DELETE FROM magi_conversation WHERE id = ?", "conv-"+messageID)
	cmd := a2a.PrepareCommand{
		SubmissionID: "sub-" + messageID, MessageID: messageID, RequestHash: "hash",
		TaskID: "case-" + messageID, ContextID: "conv-" + messageID,
		InputMessageID: "input-" + messageID, CaseMessageID: "case-msg-" + messageID,
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}

	const callers = 100
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared, created, err := repo.Prepare(context.Background(), cmd)
			if err == nil && !created && (prepared == nil || prepared.Case == nil) {
				err = fmt.Errorf("replayed prepare returned incomplete snapshot")
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
	assertMySQLCountWhere(t, db, &magi.A2ASubmissionModel{}, "message_id = ?", []any{cmd.MessageID}, 1)
	assertMySQLCountWhere(t, db, &magi.CaseModel{}, "id = ?", []any{cmd.TaskID}, 1)
	assertMySQLCountWhere(t, db, &magi.ConversationModel{}, "id = ?", []any{cmd.ContextID}, 1)
	assertMySQLCountWhere(t, db, &magi.ConversationMessageModel{}, "conversation_id = ?", []any{cmd.ContextID}, 2)

	// The durable job's case_id unique key also collapses concurrent enqueues.
	jobs := magi.NewDecisionJobRepository(db)
	jobErrCh := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, admitted, err := jobs.Admit(context.Background(), cmd.TaskID, 3, 0)
			if err == nil && job == nil {
				err = fmt.Errorf("admit returned nil job")
			}
			if err == nil && !admitted {
				err = fmt.Errorf("admit was rejected for an existing job")
			}
			jobErrCh <- err
		}()
	}
	wg.Wait()
	close(jobErrCh)
	for err := range jobErrCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertMySQLCountWhere(t, db, &magi.DecisionJobModel{}, "case_id = ?", []any{cmd.TaskID}, 1)
}

// TestA2AEventSequence_StrictlyIncreasingOnMySQL proves every persisted event
// seq for a case is unique and strictly increasing under concurrency.
func TestA2AEventSequence_StrictlyIncreasingOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatal(err)
	}
	events := magi.NewRepository(db).EventRepo()
	caseID := fmt.Sprintf("case-seq-%d", os.Getpid())
	// clean any prior run for this case id
	_ = db.Exec("DELETE FROM magi_event WHERE case_id = ?", caseID)
	_ = db.Exec("DELETE FROM magi_event_cursor WHERE case_id = ?", caseID)

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = events.Create(context.Background(), &entity.MagiEvent{
				ID: fmt.Sprintf("%s-%d", caseID, i), CaseID: caseID,
				Type: entity.EventCaseStatusChanged, Seq: 0, Timestamp: time.Now(),
			})
		}(i)
	}
	wg.Wait()

	persisted, err := events.ListByCase(context.Background(), caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != n {
		t.Fatalf("persisted events = %d, want %d", len(persisted), n)
	}
	seen := make(map[uint64]bool)
	for _, e := range persisted {
		if e.Seq == 0 {
			t.Fatal("event persisted without a seq")
		}
		if seen[e.Seq] {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = true
	}
	var prev uint64
	for i, e := range persisted {
		if i > 0 && e.Seq <= prev {
			t.Fatalf("seq not strictly increasing at %d: %d <= %d", i, e.Seq, prev)
		}
		prev = e.Seq
	}
}

func assertMySQLCountWhere(t *testing.T, db *gorm.DB, model any, query string, args []any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Where(query, args...).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}

func mysqlTerminalCaseStatus(s entity.CaseStatus) bool {
	switch s {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}

type instantOrch struct{}

func (instantOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	return &entity.Resolution{ID: "res-" + c.ID, CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

// TestA2ASubmission_ConcurrentSubmitSameMessageOnMySQL proves the real
// SubmissionService.Submit path under MySQL concurrency: 100 concurrent calls
// share the same external (userID, messageID) but generate their normal
// distinct internal IDs, and the idempotency + claim fencing collapse them into
// exactly one Case, binding, and Job with a STARTED binding.
func TestA2ASubmission_ConcurrentSubmitSameMessageOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(
		&magi.A2ASubmissionModel{}, &magi.CaseModel{}, &magi.ConversationModel{},
		&magi.ConversationMessageModel{}, &magi.DecisionJobModel{}, &magi.RunAdmissionLockModel{},
		&magi.ResolutionModel{}, &magi.EventModel{}, &magi.EventCursorModel{},
		&magi.EvidenceModel{}, &magi.ClaimModel{}, &magi.VoteModel{},
	); err != nil {
		t.Fatal(err)
	}
	messageID := fmt.Sprintf("mysql-submit-%d", os.Getpid())
	// Clean any prior run for the deterministic external message id.
	_ = db.Exec("DELETE FROM a2a_submission WHERE message_id = ?", messageID)

	repo := magi.NewA2ASubmissionRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	rm := decision.NewRunManager(instantOrch{}, decision.RunManagerDeps{
		JobRepo: jobs, MaxConcurrentRunsPerUser: 100,
	})
	parser := a2a.NewInputParser(65536, 16)
	proj := a2a.NewTaskProjector(redact.New("sk-secret"))
	svc := a2a.NewSubmissionService(parser, repo, rm, proj, 3)
	req := &a2asdk.SendMessageRequest{Message: &a2asdk.Message{
		ID: messageID, Role: a2asdk.MessageRoleUser,
		Parts: a2asdk.ContentParts{a2asdk.NewTextPart("Should MAGI expose A2A?")},
	}}

	const callers = 100
	errCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := svc.Submit(context.Background(), 7, req)
			if err == nil && (task == nil || task.ID == "") {
				err = fmt.Errorf("submit returned empty task")
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
	var sub magi.A2ASubmissionModel
	if err := db.Where("message_id = ?", messageID).First(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if sub.State != string(a2a.SubmissionStarted) {
		t.Fatalf("binding state = %s, want STARTED", sub.State)
	}
	// The internal IDs are per-call UUIDs, so scope the row counts to the
	// single durable task the concurrent submissions converged on.
	assertMySQLCountWhere(t, db, &magi.A2ASubmissionModel{}, "task_id = ?", []any{sub.TaskID}, 1)
	assertMySQLCountWhere(t, db, &magi.CaseModel{}, "id = ?", []any{sub.TaskID}, 1)
	assertMySQLCountWhere(t, db, &magi.DecisionJobModel{}, "case_id = ?", []any{sub.TaskID}, 1)
	assertMySQLCountWhere(t, db, &magi.ConversationModel{}, "id = ?", []any{sub.ContextID}, 1)
	assertMySQLCountWhere(t, db, &magi.ConversationMessageModel{}, "conversation_id = ?", []any{sub.ContextID}, 2)
}

// TestA2ASubmission_ConcurrentClaimStartOnMySQL proves the row-lock claim
// fencing on MySQL: two replicas claiming the same PREPARED binding produce
// exactly one winner, and only the winner's token can settle.
func TestA2ASubmission_ConcurrentClaimStartOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.A2ASubmissionModel{}, &magi.CaseModel{}); err != nil {
		t.Fatal(err)
	}
	repo := magi.NewA2ASubmissionRepository(db)
	subID := fmt.Sprintf("sub-claim-%d", os.Getpid())
	msgID := fmt.Sprintf("claim-msg-%d", os.Getpid())
	taskID := fmt.Sprintf("case-claim-%d", os.Getpid())
	_ = db.Exec("DELETE FROM a2a_submission WHERE message_id = ?", msgID)
	_ = db.Exec("DELETE FROM decision_case WHERE id = ?", taskID)
	cmd := a2a.PrepareCommand{
		SubmissionID: subID, MessageID: msgID, RequestHash: "hash",
		TaskID: taskID, ContextID: "", InputMessageID: "", CaseMessageID: "",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	lease := time.Now().Add(time.Minute)
	var wins int
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, claimed, err := repo.ClaimStart(context.Background(), subID, fmt.Sprintf("token-%d", i), lease)
			if err != nil {
				t.Errorf("claim %d: %v", i, err)
				return
			}
			if claimed {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent claim winners = %d, want 1", wins)
	}
}

// TestA2ASnapshot_ConsistentOnMySQL proves GetTaskRecord returns a consistent
// snapshot under MySQL REPEATABLE READ while a terminal transaction commits
// concurrently: a record never combines a pre-terminal Case with a terminal
// MaxEventSeq.
func TestA2ASnapshot_ConsistentOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.A2ASubmissionModel{}, &magi.CaseModel{}, &magi.DecisionJobModel{}, &magi.ResolutionModel{}, &magi.EventModel{}, &magi.EventCursorModel{}, &magi.EvidenceModel{}, &magi.ClaimModel{}, &magi.VoteModel{}); err != nil {
		t.Fatal(err)
	}
	repo := magi.NewA2ASubmissionRepository(db)
	caseID := fmt.Sprintf("case-snap-%d", os.Getpid())
	jobID := fmt.Sprintf("job-snap-%d", os.Getpid())
	resID := fmt.Sprintf("res-snap-%d", os.Getpid())
	evID := fmt.Sprintf("ev-snap-%d", os.Getpid())
	cmd := a2a.PrepareCommand{
		SubmissionID: fmt.Sprintf("sub-snap-%d", os.Getpid()), MessageID: fmt.Sprintf("snap-msg-%d", os.Getpid()), RequestHash: "hash",
		TaskID: caseID, ContextID: "", InputMessageID: "", CaseMessageID: "",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	_ = db.Exec("DELETE FROM a2a_submission WHERE message_id = ?", cmd.MessageID)
	_ = db.Exec("DELETE FROM a2a_submission WHERE task_id = ?", caseID)
	_ = db.Exec("DELETE FROM decision_case WHERE id = ?", caseID)
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", caseID).Update("status", string(entity.CaseStatusInvestigating)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.DecisionJobModel{ID: jobID, CaseID: caseID, Status: string(entity.DecisionJobRunning), AvailableAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	readsDone := make(chan error, 1)
	go func() {
		defer close(readsDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			rec, err := repo.GetTaskRecord(context.Background(), 7, caseID)
			if err != nil {
				readsDone <- err
				return
			}
			if rec.MaxEventSeq > 0 && !mysqlTerminalCaseStatus(rec.Case.Status) {
				readsDone <- fmt.Errorf("inconsistent snapshot: case=%s maxSeq=%d", rec.Case.Status, rec.MaxEventSeq)
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", caseID).
		Updates(map[string]any{"status": string(entity.CaseStatusResolved), "updated_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	consensus, _ := json.Marshal(entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval, Round: 1})
	if err := db.Create(&magi.ResolutionModel{ID: resID, CaseID: caseID, FinalDecision: string(entity.VoteDecisionApprove), ConsensusJSON: string(consensus)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.EventModel{ID: evID, CaseID: caseID, Seq: 1, Type: string(entity.EventCaseCompleted), Timestamp: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	close(stop)
	if err := <-readsDone; err != nil {
		t.Fatal(err)
	}
}

// TestMySQLDecisionJobAdmissionAcrossReplicas proves the durable DecisionJob
// is the concurrency truth across two DB handles at per-user limit 1: exactly
// one replica may admit a new queued job, and the other must be rate-limited.
func TestMySQLDecisionJobAdmissionAcrossReplicas(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.DecisionJobModel{}, &magi.RunAdmissionLockModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	// A user id no other MySQL test touches, plus a pre-clean of this test's own
	// leftovers: the admission limit is per user, so a queued job left behind by
	// an earlier run against the same schema would change the outcome and make
	// the suite non-reproducible outside a pristine database.
	userID := int64(907)
	clear := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatalf("clear leftovers (%s): %v", query, err)
		}
	}
	// By fixed case id (a previous revision of this test seeded them under a
	// different user id) and by this user.
	clear("DELETE FROM decision_job WHERE case_id IN ('case-admit-a','case-admit-b')")
	clear("DELETE FROM decision_case WHERE id IN ('case-admit-a','case-admit-b')")
	clear("DELETE FROM decision_job WHERE case_id IN (SELECT id FROM decision_case WHERE user_id = ?)", userID)
	clear("DELETE FROM decision_case WHERE user_id = ?", userID)
	for _, id := range []string{"case-admit-a", "case-admit-b"} {
		if err := db.Create(&magi.CaseModel{ID: id, UserID: userID, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Two independent GORM handles share the same MySQL schema.
	handleA := magi.NewDecisionJobRepository(db)
	handleB := magi.NewDecisionJobRepository(db)
	start := make(chan struct{})
	result := make(chan struct {
		id       string
		admitted bool
		err      error
	}, 2)
	admit := func(handle port.DecisionJobRepository, id string) {
		<-start
		_, admitted, err := handle.Admit(ctx, id, 3, 1)
		result <- struct {
			id       string
			admitted bool
			err      error
		}{id: id, admitted: admitted, err: err}
	}
	go admit(handleA, "case-admit-a")
	go admit(handleB, "case-admit-b")
	close(start)

	var admittedCount, deniedCount int
	for i := 0; i < 2; i++ {
		r := <-result
		if r.err != nil {
			t.Fatalf("admit %s err: %v", r.id, r.err)
		}
		if r.admitted {
			admittedCount++
		} else {
			deniedCount++
		}
	}
	if admittedCount != 1 || deniedCount != 1 {
		t.Fatalf("admitted=%d denied=%d, want exactly one admitted at limit 1", admittedCount, deniedCount)
	}

	var active int64
	if err := db.Model(&magi.DecisionJobModel{}).Joins("JOIN decision_case ON decision_case.id = decision_job.case_id").
		Where("decision_case.user_id = ? AND decision_job.status IN ?", userID,
			[]string{string(entity.DecisionJobQueued), string(entity.DecisionJobRunning)}).
		Count(&active).Error; err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Fatalf("active durable jobs = %d, want exactly 1", active)
	}
}
