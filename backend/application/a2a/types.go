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
	// ErrNotFound means an owner-scoped A2A task does not exist for the caller.
	ErrNotFound = errors.New("a2a task not found")
	// ErrContextContention means concurrent ContextID creation did not settle
	// within the bounded transaction retry budget.
	ErrContextContention = errors.New("a2a context creation contention")
	// ErrStreamLimitExceeded means the principal already holds the configured
	// number of active A2A streams.
	ErrStreamLimitExceeded = errors.New("a2a stream limit exceeded")
	// ErrClaimLost means a settlement attempted to finalize a STARTING
	// submission whose claim token no longer matches (another replica stole
	// the claim or the lease expired and was reclaimed).
	ErrClaimLost = errors.New("a2a start claim lost")
)

type SubmissionState string

const (
	SubmissionPrepared SubmissionState = "PREPARED"
	SubmissionStarting SubmissionState = "STARTING"
	SubmissionStarted  SubmissionState = "STARTED"
	SubmissionRejected SubmissionState = "REJECTED"
)

type Submission struct {
	ID, MessageID, RequestHash, TaskID, ContextID, InputMessageID string
	UserID                                                        int64
	State                                                         SubmissionState
	ErrorCode                                                     string
	AcceptedOutputModes                                           []string
	CreatedAt, UpdatedAt                                          time.Time
}

type PrepareCommand struct {
	SubmissionID, MessageID, RequestHash, TaskID, ContextID string
	InputMessageID, CaseMessageID                           string
	UserID                                                  int64
	Question, Background                                    string
	Constraints                                             []entity.Constraint
	AcceptedOutputModes                                     []string
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
	Votes        []*entity.Vote
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

// CancelResult is the durable outcome of a cancellation. Event is the ordered
// durable CANCELLED event committed by CancelTask when cancellation applied; it
// is nil for already-canceled, terminal, or not-found outcomes. Record is the
// owner-scoped Task snapshot read in the same transaction as the cancel.
type CancelResult struct {
	Record  *TaskRecord
	Outcome CancelOutcome
	Event   *entity.MagiEvent
}

type SubmissionRepository interface {
	Prepare(context.Context, PrepareCommand) (*PreparedSubmission, bool, error)
	// ClaimStart atomically claims a PREPARED or expired-STARTING binding with
	// a fresh token and lease and returns the locked prepared snapshot. A false
	// result means the binding is already STARTED/REJECTED or another replica
	// holds a live claim.
	ClaimStart(context.Context, string, string, time.Time) (*PreparedSubmission, bool, error)
	// SettleStarted finalizes a STARTING binding to STARTED. It only succeeds
	// while the caller still owns the claim token.
	SettleStarted(context.Context, string, string) error
	// SettleRejected finalizes a STARTING binding to REJECTED with a stable
	// code. It only succeeds while the caller still owns the claim token.
	SettleRejected(context.Context, string, string, string) error
	// ListClaimable returns PREPARED and expired-STARTING bindings ordered by
	// id, starting strictly after afterID for keyset paging.
	ListClaimable(context.Context, int, string) ([]*PreparedSubmission, error)
	GetByTask(context.Context, int64, string) (*Submission, error)
	GetTaskRecord(context.Context, int64, string) (*TaskRecord, error)
	ListTasks(context.Context, TaskListFilter) (*TaskPage, error)
	CancelTask(context.Context, int64, string) (*CancelResult, error)
}
