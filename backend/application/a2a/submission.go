package a2aapp

import (
	"context"
	"errors"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
)

// SubmissionService owns the durable A2A submit lifecycle: parse, prepare,
// idempotent start, and recovery of PREPARED bindings after a crash.
type SubmissionService struct {
	parser             InputParser
	repo               SubmissionRepository
	runManager         *decision.RunManager
	projector          *TaskProjector
	maxDebateRounds    int
	startTimeout       time.Duration
	claimLease         time.Duration
	recoveryInterval   time.Duration
	recoveryBackoff    time.Duration
	recoveryMaxBackoff time.Duration
	metrics            *metrics.Registry
}

// SubmissionOption configures optional observability on the submission service.
type SubmissionOption func(*SubmissionService)

// WithSubmissionMetrics enables the idempotency-hit counter.
func WithSubmissionMetrics(reg *metrics.Registry) SubmissionOption {
	return func(s *SubmissionService) { s.metrics = reg }
}

// WithStartTimeout bounds how long a claim settlement may run.
func WithStartTimeout(d time.Duration) SubmissionOption {
	return func(s *SubmissionService) {
		if d > 0 {
			s.startTimeout = d
		}
	}
}

// WithClaimLease sets how long a STARTING claim may be held before it is
// reclaimable by another replica or the recovery worker.
func WithClaimLease(d time.Duration) SubmissionOption {
	return func(s *SubmissionService) {
		if d > 0 {
			s.claimLease = d
		}
	}
}

// WithRecoveryInterval sets how often the background recovery sweep runs.
func WithRecoveryInterval(d time.Duration) SubmissionOption {
	return func(s *SubmissionService) {
		if d > 0 {
			s.recoveryInterval = d
		}
	}
}

func NewSubmissionService(parser InputParser, repo SubmissionRepository, runManager *decision.RunManager, projector *TaskProjector, maxDebateRounds int, opts ...SubmissionOption) *SubmissionService {
	svc := &SubmissionService{
		parser: parser, repo: repo, runManager: runManager, projector: projector,
		maxDebateRounds: maxDebateRounds, startTimeout: 30 * time.Second,
		claimLease: 30 * time.Second, recoveryInterval: 30 * time.Second,
		recoveryBackoff: time.Second, recoveryMaxBackoff: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
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
	prepared, created, err := s.repo.Prepare(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if !created && s.metrics != nil {
		s.metrics.IncA2AIdempotencyHit()
	}
	if _, err := s.settleStart(ctx, prepared); err != nil {
		return nil, err
	}
	record, err := s.repo.GetTaskRecord(ctx, prepared.Binding.UserID, prepared.Binding.TaskID)
	if err != nil {
		return nil, err
	}
	return s.projector.Project(record, 1, true), nil
}

// settlement describes how a claimable binding left its waiting state.
type settlement int

const (
	settlementUnchanged settlement = iota // already settled or claim held by another replica
	settlementStarted                     // finalized STARTED
	settlementRejected                    // finalized REJECTED with a stable code
	settlementTransient                   // transient failure: lease left for recovery
)

// settleStart claims a PREPARED or expired-STARTING binding, then finalizes it
// with a conditional write that only the claim owner can perform. Transient
// failures leave the lease in place for the recovery worker; a claim held by
// another replica is left untouched. The bounded child of the caller/lifecycle
// context keeps request cancellation from leaking a live claim forever.
func (s *SubmissionService) settleStart(ctx context.Context, prepared *PreparedSubmission) (settlement, error) {
	switch prepared.Binding.State {
	case SubmissionStarted, SubmissionRejected:
		// Already settled; the projection is authoritative.
		return settlementUnchanged, nil
	case SubmissionStarting:
		// A live claim may still be held. If it is ours this call is a replay
		// of an in-flight settlement; ClaimStart below fails to steal a live
		// lease, which is the correct idempotent behavior.
	}
	token := uuid.NewString()
	leaseUntil := time.Now().Add(s.claimLease)
	locked, claimed, err := s.repo.ClaimStart(ctx, prepared.Binding.ID, token, leaseUntil)
	if err != nil {
		return settlementUnchanged, err
	}
	if !claimed || locked == nil {
		// Another replica owns a live claim (or the binding was already
		// settled); recovery or the owner will finalize it.
		return settlementUnchanged, nil
	}
	startCtx, cancel := context.WithTimeout(ctx, s.startTimeout)
	defer cancel()
	startErr := s.runManager.EnsureStarted(startCtx, locked.Case)
	switch {
	case startErr == nil || errors.Is(startErr, decision.ErrAlreadyRunning) || errors.Is(startErr, decision.ErrAlreadyCompleted) || errors.Is(startErr, decision.ErrJobTerminated):
		// The durable job already exists. STARTED describes admission/binding;
		// the projector derives failed, cancelled, or paused from Case + Job.
		if err := s.repo.SettleStarted(ctx, locked.Binding.ID, token); err != nil {
			if errors.Is(err, ErrClaimLost) {
				// The winner already settled; our projection is stale.
				return settlementUnchanged, nil
			}
			return settlementUnchanged, err
		}
		if s.metrics != nil {
			s.metrics.IncA2ARecoverySettled()
		}
		return settlementStarted, nil
	case errors.Is(startErr, decision.ErrRateLimited) || errors.Is(startErr, decision.ErrBudgetExceeded):
		code := "rate_limited"
		if errors.Is(startErr, decision.ErrBudgetExceeded) {
			code = "budget_exceeded"
		}
		if err := s.repo.SettleRejected(ctx, locked.Binding.ID, token, code); err != nil {
			if errors.Is(err, ErrClaimLost) {
				return settlementUnchanged, nil
			}
			return settlementUnchanged, err
		}
		if s.metrics != nil {
			s.metrics.IncA2ARecoverySettled()
		}
		return settlementRejected, nil
	default:
		// Transient start failure: keep the STARTING lease for the recovery
		// worker to reclaim once it expires.
		return settlementTransient, nil
	}
}

// Recover replays the start classifier over every claimable binding so a crash
// between Prepare and start settles durably. It pages by keyset past the first
// 100 rows so transient failures on early rows cannot starve later ones.
func (s *SubmissionService) Recover(ctx context.Context) error {
	afterID := ""
	for {
		batch, err := s.repo.ListClaimable(ctx, 100, afterID)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		if s.metrics != nil {
			s.metrics.IncA2ARecoveryRetried()
		}
		for _, prepared := range batch {
			if s.metrics != nil {
				s.metrics.IncA2ARecoveryAttempted()
			}
			if _, err := s.settleStart(ctx, prepared); err != nil {
				return err
			}
		}
		afterID = batch[len(batch)-1].Binding.ID
	}
}

// RunRecovery continuously sweeps claimable bindings on a ticker, using a
// capped backoff when a sweep fails, until ctx is cancelled. It is the
// lifecycle-owned worker that retries transiently failed startup leases.
func (s *SubmissionService) RunRecovery(ctx context.Context) error {
	ticker := time.NewTicker(s.recoveryInterval)
	defer ticker.Stop()
	backoff := s.recoveryBackoff
	for {
		if err := s.Recover(ctx); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff < s.recoveryMaxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = s.recoveryBackoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
