package magi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	a2a "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
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
	record, outcome, err := repo.CancelTask(ctx, 8, "task-1")
	if err != nil || record != nil || outcome != a2a.CancelNotFound {
		t.Fatalf("foreign cancel = (%+v, %q, %v), want not found", record, outcome, err)
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
