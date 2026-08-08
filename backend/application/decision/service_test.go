package decision_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
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
