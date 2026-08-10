package recurring

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service manages recurring decision templates and fires due runs through the
// async run manager. Every run creates a fresh owner-scoped DecisionCase.
type Service struct {
	repo            port.RecurringRepository
	cases           port.CaseRepository
	runs            *decision.RunManager
	maxDebateRounds int
}

func NewService(repo port.RecurringRepository, cases port.CaseRepository, runs *decision.RunManager, maxDebateRounds int) *Service {
	return &Service{repo: repo, cases: cases, runs: runs, maxDebateRounds: maxDebateRounds}
}

func (s *Service) Create(ctx context.Context, userID int64, name, question, background string, constraints []entity.Constraint, interval time.Duration) (*entity.RecurringCase, error) {
	if userID == 0 {
		return nil, fmt.Errorf("recurring: authenticated user is required")
	}
	if name == "" || question == "" {
		return nil, fmt.Errorf("recurring: name and question are required")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("recurring: interval must be positive")
	}
	rc := &entity.RecurringCase{ID: "rc-" + uuid.NewString(), UserID: userID, Name: name, Question: question, Background: background, Constraints: constraints, Interval: interval, Enabled: true, CreatedAt: time.Now()}
	if err := s.repo.Create(ctx, rc); err != nil {
		return nil, fmt.Errorf("recurring: create: %w", err)
	}
	return rc, nil
}

func (s *Service) List(ctx context.Context, userID int64) ([]*entity.RecurringCase, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *Service) Get(ctx context.Context, userID int64, id string) (*entity.RecurringCase, error) {
	rc, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("recurring: get: %w", err)
	}
	if rc.UserID != userID {
		return nil, fmt.Errorf("recurring: forbidden")
	}
	return rc, nil
}

func (s *Service) SetEnabled(ctx context.Context, userID int64, id string, enabled bool) error {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return err
	}
	return s.repo.UpdateEnabled(ctx, id, enabled)
}

func (s *Service) Delete(ctx context.Context, userID int64, id string) error {
	if _, err := s.Get(ctx, userID, id); err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

// RunNow fires an immediate run for a template regardless of interval.
func (s *Service) RunNow(ctx context.Context, userID int64, id string) (*entity.DecisionCase, error) {
	rc, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	case_, err := s.createAndRun(ctx, rc)
	if err != nil {
		return nil, err
	}
	return case_, nil
}

// Tick fires every enabled template whose interval has elapsed.
func (s *Service) Tick(ctx context.Context, now time.Time) error {
	all, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return err
	}
	for _, rc := range all {
		if !rc.Due(now) {
			continue
		}
		if _, err := s.createAndRun(ctx, rc); err != nil {
			// A failed launch (e.g. rate-limited or budget-rejected) must NOT
			// advance last_run: the template stays due so the next tick retries
			// instead of silently skipping an interval.
			log.Printf("recurring: template %s (%s) failed to launch: %v", rc.ID, rc.Name, err)
			continue
		}
		_ = s.repo.UpdateLastRun(ctx, rc.ID, now)
	}
	return nil
}

func (s *Service) createAndRun(ctx context.Context, rc *entity.RecurringCase) (*entity.DecisionCase, error) {
	case_ := &entity.DecisionCase{
		ID: "case-" + uuid.NewString(), UserID: rc.UserID, Question: rc.Question, Context: rc.Background,
		Constraints: rc.Constraints, MaxDebateRounds: s.maxDebateRounds,
		Status: entity.CaseStatusDraft, CreatedAt: time.Now(),
	}
	if s.cases != nil {
		if err := s.cases.Create(ctx, case_); err != nil {
			return nil, fmt.Errorf("recurring: persist case: %w", err)
		}
	}
	if s.runs != nil {
		if err := s.runs.Start(ctx, case_); err != nil {
			return nil, fmt.Errorf("recurring: start run: %w", err)
		}
	}
	return case_, nil
}
