package a2aapp

import (
	"context"
	"errors"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jamespud/magi/backend/application/decision"
)

// SubmissionService owns the durable A2A submit lifecycle: parse, prepare,
// idempotent start, and recovery of PREPARED bindings after a crash.
type SubmissionService struct {
	parser          InputParser
	repo            SubmissionRepository
	runManager      *decision.RunManager
	projector       *TaskProjector
	maxDebateRounds int
	startTimeout    time.Duration
}

func NewSubmissionService(parser InputParser, repo SubmissionRepository, runManager *decision.RunManager, projector *TaskProjector, maxDebateRounds int) *SubmissionService {
	return &SubmissionService{
		parser: parser, repo: repo, runManager: runManager, projector: projector,
		maxDebateRounds: maxDebateRounds, startTimeout: 30 * time.Second,
	}
}

// Submit parses a request, durably prepares the binding, then idempotently
// starts the background case. It returns the current Task snapshot. A transient
// start failure leaves the binding PREPARED for Recover without failing Submit.
func (s *SubmissionService) Submit(ctx context.Context, userID int64, req *a2a.SendMessageRequest) (*a2a.Task, error) {
	parsed, err := s.parser.Parse(req)
	if err != nil {
		return nil, err
	}
	contextID := parsed.ContextID
	if contextID == "" {
		contextID = "conv-" + uuid.NewString()
	}
	cmd := PrepareCommand{
		SubmissionID:    "sub-" + uuid.NewString(),
		MessageID:       parsed.MessageID,
		RequestHash:     parsed.RequestHash,
		TaskID:          "case-" + uuid.NewString(),
		ContextID:       contextID,
		InputMessageID:  "msg-" + uuid.NewString(),
		CaseMessageID:   "msg-" + uuid.NewString(),
		UserID:          userID,
		Question:        parsed.Question,
		Background:      parsed.Background,
		Constraints:     parsed.Constraints,
		MaxDebateRounds: s.maxDebateRounds,
	}
	prepared, _, err := s.repo.Prepare(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if _, err := s.settleStart(ctx, prepared); err != nil {
		return nil, err
	}
	record, err := s.repo.GetTaskRecord(ctx, prepared.Binding.UserID, prepared.Binding.TaskID)
	if err != nil {
		return nil, err
	}
	return s.projector.Project(record, 1), nil
}

// settlement describes how a PREPARED binding left its waiting state.
type settlement int

const (
	settlementPrepared settlement = iota // transient: kept PREPARED for recovery
	settlementStarted                    // marked STARTED
	settlementRejected                   // marked REJECTED with a stable code
)

// settleStart classifies the RunManager start outcome. The start transition
// runs on a bounded background context so request cancellation after a
// successful Prepare never cancels the durable Case.
func (s *SubmissionService) settleStart(ctx context.Context, prepared *PreparedSubmission) (settlement, error) {
	startCtx, cancel := context.WithTimeout(context.Background(), s.startTimeout)
	defer cancel()
	startErr := s.runManager.EnsureStarted(startCtx, prepared.Case)
	switch {
	case startErr == nil || errors.Is(startErr, decision.ErrAlreadyRunning) || errors.Is(startErr, decision.ErrAlreadyCompleted):
		if err := s.repo.MarkStarted(ctx, prepared.Binding.ID); err != nil {
			return settlementPrepared, err
		}
		return settlementStarted, nil
	case errors.Is(startErr, decision.ErrRateLimited) || errors.Is(startErr, decision.ErrBudgetExceeded):
		code := "rate_limited"
		if errors.Is(startErr, decision.ErrBudgetExceeded) {
			code = "budget_exceeded"
		}
		if err := s.repo.MarkRejected(ctx, prepared.Binding.ID, code); err != nil {
			return settlementPrepared, err
		}
		return settlementRejected, nil
	default:
		return settlementPrepared, nil
	}
}

// Recover replays the start classifier over every PREPARED binding so a crash
// between Prepare and start settles durably. It stops when a batch makes no
// progress so a transient failure cannot spin the recovery loop.
func (s *SubmissionService) Recover(ctx context.Context) error {
	for {
		batch, err := s.repo.ListPrepared(ctx, 100)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		progress := false
		for _, prepared := range batch {
			settled, err := s.settleStart(ctx, prepared)
			if err != nil {
				return err
			}
			if settled != settlementPrepared {
				progress = true
			}
		}
		if !progress {
			return nil
		}
	}
}
