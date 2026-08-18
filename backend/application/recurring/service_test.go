package recurring_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/domain/entity"
)

type stubRecurringRepo struct {
	mu      sync.Mutex
	items   map[string]*entity.RecurringCase
	enabled []string
}

func newStubRecurringRepo() *stubRecurringRepo {
	return &stubRecurringRepo{items: make(map[string]*entity.RecurringCase)}
}

func (s *stubRecurringRepo) Create(ctx context.Context, r *entity.RecurringCase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[r.ID] = r
	return nil
}
func (s *stubRecurringRepo) Get(ctx context.Context, id string) (*entity.RecurringCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.items[id]; ok {
		return r, nil
	}
	return nil, errors.New("not found")
}
func (s *stubRecurringRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.RecurringCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entity.RecurringCase
	for _, r := range s.items {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *stubRecurringRepo) ListEnabled(ctx context.Context) ([]*entity.RecurringCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entity.RecurringCase
	for _, r := range s.items {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *stubRecurringRepo) Update(ctx context.Context, r *entity.RecurringCase) error { return nil }
func (s *stubRecurringRepo) UpdateEnabled(ctx context.Context, id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.items[id]; ok {
		r.Enabled = enabled
	}
	return nil
}
func (s *stubRecurringRepo) UpdateLastRun(ctx context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.items[id]; ok {
		r.LastRunAt = &at
	}
	return nil
}
func (s *stubRecurringRepo) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
	return nil
}

type stubCases struct {
	mu      sync.Mutex
	created []*entity.DecisionCase
}

func (s *stubCases) Create(ctx context.Context, c *entity.DecisionCase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, c)
	return nil
}
func (s *stubCases) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *stubCases) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *stubCases) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *stubCases) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

func (s *stubCases) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *stubCases) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *stubCases) Delete(ctx context.Context, id string) error { return nil }

type stubRecOrch struct{}

func (stubRecOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestRecurring_TickFiresDueAndSkipsOthers(t *testing.T) {
	repo := newStubRecurringRepo()
	cases := &stubCases{}
	rm := decision.NewRunManager(stubRecOrch{}, decision.RunManagerDeps{})
	svc := recurring.NewService(repo, cases, rm, 1)
	ctx := context.Background()

	due, _ := svc.Create(ctx, 1, "daily review", "Should we keep the current stack?", "", nil, time.Minute)
	_ = due
	notDue, _ := svc.Create(ctx, 1, "weekly review", "Should we expand?", "", nil, time.Hour)
	now := time.Now()
	repo.items[notDue.ID].LastRunAt = &now // just ran, not due
	disabled, _ := svc.Create(ctx, 1, "off", "Should we disable?", "", nil, time.Second)
	_ = svc.SetEnabled(ctx, 1, disabled.ID, false)

	if err := svc.Tick(ctx, now); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(cases.created) != 1 {
		t.Fatalf("expected exactly one case created, got %d", len(cases.created))
	}
	if cases.created[0].UserID != 1 {
		t.Fatalf("case owner: %d", cases.created[0].UserID)
	}
	dueGot, _ := repo.Get(ctx, due.ID)
	if dueGot.LastRunAt == nil {
		t.Fatal("due template last_run_at must be updated")
	}
}

func TestRecurring_OwnershipEnforced(t *testing.T) {
	repo := newStubRecurringRepo()
	svc := recurring.NewService(repo, &stubCases{}, nil, 1)
	ctx := context.Background()
	r, _ := svc.Create(ctx, 1, "private", "q?", "", nil, time.Minute)
	if _, err := svc.Get(ctx, 2, r.ID); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	if err := svc.SetEnabled(ctx, 2, r.ID, false); err == nil {
		t.Fatal("expected forbidden for other user")
	}
	if _, err := svc.RunNow(ctx, 2, r.ID); err == nil {
		t.Fatal("expected forbidden for other user")
	}
}
