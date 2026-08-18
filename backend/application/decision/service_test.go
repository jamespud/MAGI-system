package decision_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubOrchestrator struct {
	result *entity.Resolution
	err    error
}

func (s *stubOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	return s.result, s.err
}

type stubCaseRepo struct {
	case_       *entity.DecisionCase
	cancelledID string
	created     []*entity.DecisionCase
}

type pauseCaseRepo struct {
	case_ *entity.DecisionCase
}

func (s *pauseCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *pauseCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.case_, nil
}
func (s *pauseCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *pauseCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *pauseCaseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	s.case_.Status = status
	return nil
}
func (s *pauseCaseRepo) UpdatePaused(ctx context.Context, id string, status, pausedFrom entity.CaseStatus) error {
	s.case_.Status = status
	s.case_.PausedFromStatus = pausedFrom
	return nil
}
func (s *pauseCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *pauseCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *pauseCaseRepo) Delete(ctx context.Context, id string) error { return nil }

var _ port.CaseRepository = (*pauseCaseRepo)(nil)
var _ port.PauseStatusWriter = (*pauseCaseRepo)(nil)

func TestService_PauseAndResume(t *testing.T) {
	repo := &pauseCaseRepo{case_: &entity.DecisionCase{ID: "c1", Status: entity.CaseStatusInvestigating}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	ctx := context.Background()

	if err := svc.Pause(ctx, "c1"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if repo.case_.Status != entity.CaseStatusPaused || repo.case_.PausedFromStatus != entity.CaseStatusInvestigating {
		t.Fatalf("after pause = %+v", repo.case_)
	}
	if err := svc.Pause(ctx, "c1"); err == nil {
		t.Fatal("pausing an already paused case must fail")
	}

	if err := svc.Resume(ctx, "c1"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if repo.case_.Status != entity.CaseStatusInvestigating || repo.case_.PausedFromStatus != "" {
		t.Fatalf("after resume = %+v", repo.case_)
	}
	if err := svc.Resume(ctx, "c1"); err == nil {
		t.Fatal("resuming a non-paused case must fail")
	}
}

func (s *stubCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error {
	s.created = append(s.created, c)
	return nil
}
func (s *stubCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.case_, nil
}
func (s *stubCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	if s.case_ == nil {
		return nil, nil
	}
	return []*entity.DecisionCase{s.case_}, nil
}
func (s *stubCaseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	s.cancelledID = id
	return nil
}
func (s *stubCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

func (s *stubCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *stubCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *stubCaseRepo) Delete(ctx context.Context, id string) error { return nil }

func TestService_Create(t *testing.T) {
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{MaxDebateRounds: 2})
	c, err := svc.Create(context.Background(), 0, "test question", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Question != "test question" {
		t.Fatalf("question: %s", c.Question)
	}
	if c.MaxDebateRounds != 2 {
		t.Fatalf("maxDebateRounds: %d", c.MaxDebateRounds)
	}
	if c.Status != entity.CaseStatusDraft {
		t.Fatalf("status: %s", c.Status)
	}
}

func TestService_Create_UsesUniqueIDs(t *testing.T) {
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{})
	first, _ := svc.Create(context.Background(), 0, "q1", "", nil)
	second, _ := svc.Create(context.Background(), 0, "q2", "", nil)
	if first.ID == second.ID || len(first.ID) <= len("case-") || len(second.ID) <= len("case-") {
		t.Fatalf("case IDs are not unique/usable: %q %q", first.ID, second.ID)
	}
}
func TestService_Create_WithBackgroundAndConstraints(t *testing.T) {
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{MaxDebateRounds: 3})
	c, err := svc.Create(context.Background(), 0, "Should we adopt Rust?",
		"Java backend team of 5",
		[]entity.Constraint{{Key: "Budget", Value: "3 months", Hard: false}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Question != "Should we adopt Rust?" {
		t.Fatalf("question: got %q", c.Question)
	}
	if c.Context != "Java backend team of 5" {
		t.Fatalf("context/background: got %q", c.Context)
	}
	if len(c.Constraints) != 1 || c.Constraints[0].Key != "Budget" || c.Constraints[0].Value != "3 months" {
		t.Fatalf("constraints: got %+v", c.Constraints)
	}
	if c.Status != entity.CaseStatusDraft {
		t.Fatalf("status: got %s", c.Status)
	}
}

func TestService_Run(t *testing.T) {
	want := &entity.Resolution{FinalDecision: entity.VoteDecisionApprove}
	svc := decision.NewService(&stubOrchestrator{result: want}, decision.ServiceConfig{MaxDebateRounds: 2})
	c, _ := svc.Create(context.Background(), 0, "q", "", nil)
	got, err := svc.Run(context.Background(), c)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got.FinalDecision != want.FinalDecision {
		t.Fatalf("decision: %s", got.FinalDecision)
	}
}

func TestService_Get(t *testing.T) {
	repo := &stubCaseRepo{case_: &entity.DecisionCase{ID: "c1", Question: "found"}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	got, err := svc.Get(context.Background(), "c1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != "c1" {
		t.Fatalf("expected c1, got: %+v", got)
	}
}

func TestService_ForkAndRun(t *testing.T) {
	repo := &stubCaseRepo{case_: &entity.DecisionCase{
		ID: "src", Question: "Should I adopt a dog?", Context: "one dog", MaxDebateRounds: 3,
		Constraints: []entity.Constraint{{Key: "budget", Value: "small"}},
	}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{MaxDebateRounds: 2}, decision.WithCaseRepo(repo))

	forked, err := svc.ForkAndRun(context.Background(), 7, "src")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if forked.ID == "src" {
		t.Fatal("fork must be a new case")
	}
	if forked.ParentCaseID != "src" {
		t.Fatalf("parent link: got %q want src", forked.ParentCaseID)
	}
	if forked.Question != "Should I adopt a dog?" || forked.Context != "one dog" {
		t.Fatalf("fork must inherit question/context, got %q / %q", forked.Question, forked.Context)
	}
	if len(forked.Constraints) != 1 || forked.Constraints[0].Key != "budget" {
		t.Fatalf("fork must inherit constraints: %+v", forked.Constraints)
	}
	if forked.UserID != 7 {
		t.Fatalf("fork owner: got %d want 7", forked.UserID)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected the fork to be persisted, got %d created", len(repo.created))
	}
}

func TestService_ForkAndRun_MissingSource(t *testing.T) {
	repo := &stubCaseRepo{} // Get returns nil
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	if _, err := svc.ForkAndRun(context.Background(), 1, "missing"); err == nil {
		t.Fatal("expected error for missing source case")
	}
}
func TestService_Cancel(t *testing.T) {
	repo := &stubCaseRepo{}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))
	err := svc.Cancel(context.Background(), "c1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if repo.cancelledID != "c1" {
		t.Fatalf("expected cancel c1, got %s", repo.cancelledID)
	}
}

// multiCaseRepo implements port.CaseRepository but NOT port.CaseListFilter, so
// ListScoped must fall back to the full List + in-memory owner filter + paging.
type multiCaseRepo struct {
	cases []*entity.DecisionCase
}

func (s *multiCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *multiCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *multiCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	return s.cases, nil
}
func (s *multiCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *multiCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *multiCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *multiCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *multiCaseRepo) Delete(ctx context.Context, id string) error { return nil }

// scopedCaseRepo implements port.CaseListFilter; ListScoped must delegate to it.
type scopedCaseRepo struct {
	lastUserID int64
	lastLimit  int
	lastOffset int
	cases      []*entity.DecisionCase
}

func (s *scopedCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *scopedCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return nil, nil
}
func (s *scopedCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	return s.cases, nil
}
func (s *scopedCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (s *scopedCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *scopedCaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *scopedCaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *scopedCaseRepo) Delete(ctx context.Context, id string) error { return nil }
func (s *scopedCaseRepo) ListForUser(ctx context.Context, userID int64, limit, offset int) ([]*entity.DecisionCase, error) {
	s.lastUserID, s.lastLimit, s.lastOffset = userID, limit, offset
	return s.cases, nil
}

// TestService_ListScoped_FallbackFiltersByOwner verifies the in-memory fallback
// (repos without port.CaseListFilter) scopes by owner and pages (P0: D2).
func TestService_ListScoped_FallbackFiltersByOwner(t *testing.T) {
	repo := &multiCaseRepo{cases: []*entity.DecisionCase{
		{ID: "mine", UserID: 7},
		{ID: "other", UserID: 8},
		{ID: "open", UserID: 0},
		{ID: "mine2", UserID: 7},
	}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))

	out, err := svc.ListScoped(context.Background(), 7, 100, 0)
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 (own+open), got %d", len(out))
	}
	for _, c := range out {
		if c.ID == "other" {
			t.Fatalf("must not return another user's case")
		}
	}

	// open mode sees everything
	outOpen, err := svc.ListScoped(context.Background(), 0, 100, 0)
	if err != nil || len(outOpen) != 4 {
		t.Fatalf("open mode expected 4, got %d err=%v", len(outOpen), err)
	}
}

func TestService_ListScoped_FallbackPaginates(t *testing.T) {
	repo := &multiCaseRepo{cases: []*entity.DecisionCase{
		{ID: "c1", UserID: 7}, {ID: "c2", UserID: 7}, {ID: "c3", UserID: 7}, {ID: "c4", UserID: 7},
	}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))

	page1, err := svc.ListScoped(context.Background(), 7, 2, 0)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1: %v %d", err, len(page1))
	}
	page2, err := svc.ListScoped(context.Background(), 7, 2, 2)
	if err != nil || len(page2) != 2 {
		t.Fatalf("page2: %v %d", err, len(page2))
	}
	seen := map[string]bool{}
	for _, p := range [][]*entity.DecisionCase{page1, page2} {
		for _, c := range p {
			seen[c.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 unique across pages, got %d", len(seen))
	}
}

// TestService_ListScoped_DelegatesToRepo verifies repos implementing
// port.CaseListFilter are called directly with the principal's userID (the SQL
// scoping path; P0: D2).
func TestService_ListScoped_DelegatesToRepo(t *testing.T) {
	repo := &scopedCaseRepo{cases: []*entity.DecisionCase{{ID: "mine", UserID: 7}}}
	svc := decision.NewService(&stubOrchestrator{}, decision.ServiceConfig{}, decision.WithCaseRepo(repo))

	out, err := svc.ListScoped(context.Background(), 7, 25, 10)
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected delegation result, got %d", len(out))
	}
	if repo.lastUserID != 7 || repo.lastLimit != 25 || repo.lastOffset != 10 {
		t.Fatalf("ListForUser not called with expected args: %d %d %d", repo.lastUserID, repo.lastLimit, repo.lastOffset)
	}
}
