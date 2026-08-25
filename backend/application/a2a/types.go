// Package a2aapp owns A2A-facing application contracts without depending on
// the transport SDK. MAGI adapters use these records to persist task bindings.
package a2aapp

import (
	"context"
	"errors"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
)

var (
	// ErrIdempotencyConflict means an A2A message ID was replayed with a
	// different canonical request hash.
	ErrIdempotencyConflict = errors.New("a2a idempotency conflict")
	// ErrForbidden means a referenced A2A context belongs to another principal.
	ErrForbidden = errors.New("a2a forbidden")
	// ErrContextContention means concurrent ContextID creation did not settle
	// within the bounded transaction retry budget.
	ErrContextContention = errors.New("a2a context creation contention")
)

type SubmissionState string

const (
	SubmissionPrepared SubmissionState = "PREPARED"
	SubmissionStarted  SubmissionState = "STARTED"
	SubmissionRejected SubmissionState = "REJECTED"
)

type Submission struct {
	ID, MessageID, RequestHash, TaskID, ContextID, InputMessageID string
	UserID                                                        int64
	State                                                         SubmissionState
	ErrorCode                                                     string
	CreatedAt, UpdatedAt                                          time.Time
}

type PrepareCommand struct {
	SubmissionID, MessageID, RequestHash, TaskID, ContextID string
	InputMessageID, CaseMessageID                           string
	UserID                                                  int64
	Question, Background                                    string
	Constraints                                             []entity.Constraint
	MaxDebateRounds                                         int
}

type PreparedSubmission struct {
	Binding      Submission
	Case         *entity.DecisionCase
	Conversation *entity.Conversation
	InputMessage *entity.ConversationMessage
	CaseMessage  *entity.ConversationMessage
}

type TaskCursor struct {
	CreatedAt time.Time
	ID        string
}

type TaskListFilter struct {
	UserID               int64
	ContextID, Status    string
	StatusTimestampAfter *time.Time
	IncludeArtifacts     bool
	HistoryLength        int
	Limit                int
	After                *TaskCursor
}

type TaskRecord struct {
	Submission   Submission
	Case         *entity.DecisionCase
	Job          *entity.DecisionJob
	Resolution   *entity.Resolution
	InputMessage *entity.ConversationMessage
	Evidence     []*entity.EvidenceRecord
	Claims       []*entity.Claim
	MaxEventSeq  uint64
}

type TaskPage struct {
	Records []TaskRecord
	Next    *TaskCursor
	Total   int
}

type CancelOutcome string

const (
	CancelApplied         CancelOutcome = "applied"
	CancelAlreadyCanceled CancelOutcome = "already_canceled"
	CancelNotCancelable   CancelOutcome = "not_cancelable"
	CancelNotFound        CancelOutcome = "not_found"
)

type SubmissionRepository interface {
	Prepare(context.Context, PrepareCommand) (*PreparedSubmission, bool, error)
	MarkStarted(context.Context, string) error
	MarkRejected(context.Context, string, string) error
	ListPrepared(context.Context, int) ([]*PreparedSubmission, error)
	GetByTask(context.Context, int64, string) (*Submission, error)
	ListTasks(context.Context, TaskListFilter) (*TaskPage, error)
	CancelTask(context.Context, int64, string) (*TaskRecord, CancelOutcome, error)
}
