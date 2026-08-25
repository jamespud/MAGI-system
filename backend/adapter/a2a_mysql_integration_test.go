package magi_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	a2a "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func openA2AMySQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("MAGI_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("MAGI_TEST_MYSQL_DSN not set; skipping MySQL integration test")
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
	assertMySQLCounts(t, db, &magi.A2ASubmissionModel{}, 1)
	assertMySQLCounts(t, db, &magi.CaseModel{}, 1)
	assertMySQLCounts(t, db, &magi.ConversationModel{}, 1)
	assertMySQLCounts(t, db, &magi.ConversationMessageModel{}, 2)

	// The durable job's case_id unique key also collapses concurrent enqueues.
	jobs := magi.NewDecisionJobRepository(db)
	jobErrCh := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := jobs.Enqueue(context.Background(), cmd.TaskID, 3)
			if err == nil && job == nil {
				err = fmt.Errorf("enqueue returned nil job")
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
	assertMySQLCounts(t, db, &magi.DecisionJobModel{}, 1)
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
				Type: entity.EventCaseStatusChanged, Seq: 0,
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

func assertMySQLCounts(t *testing.T, db *gorm.DB, model any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Count(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%T count = %d, want %d", model, got, want)
	}
}
