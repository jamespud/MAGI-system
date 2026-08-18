package selfimprove_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/selfimprove"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type stubSIRepo struct {
	items map[string]*entity.SelfImproveSuggestion
}

func (s *stubSIRepo) Create(ctx context.Context, it *entity.SelfImproveSuggestion) error {
	s.items[it.ID] = it
	return nil
}
func (s *stubSIRepo) List(ctx context.Context) ([]*entity.SelfImproveSuggestion, error) {
	out := make([]*entity.SelfImproveSuggestion, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, it)
	}
	return out, nil
}
func (s *stubSIRepo) Get(ctx context.Context, id string) (*entity.SelfImproveSuggestion, error) {
	return s.items[id], nil
}
func (s *stubSIRepo) UpdateStatus(ctx context.Context, id, status string) error {
	if it, ok := s.items[id]; ok {
		it.Status = status
	}
	return nil
}

type stubSICaseRepo struct {
	c *entity.DecisionCase
}

func (s *stubSICaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s *stubSICaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	return s.c, nil
}
func (s *stubSICaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s *stubSICaseRepo) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s *stubSICaseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	return nil
}
func (s *stubSICaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}
func (s *stubSICaseRepo) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s *stubSICaseRepo) Delete(ctx context.Context, id string) error { return nil }

type stubSIEventRepo struct {
	events []*entity.MagiEvent
}

func (s *stubSIEventRepo) Create(ctx context.Context, e *entity.MagiEvent) error { return nil }
func (s *stubSIEventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	return s.events, nil
}
func (s *stubSIEventRepo) ListAfter(ctx context.Context, caseID string, after time.Time) ([]*entity.MagiEvent, error) {
	return s.events, nil
}

type stubSIAgentRunRepo struct {
	runs []*entity.AgentRun
}

func (s *stubSIAgentRunRepo) Create(ctx context.Context, r *entity.AgentRun) error { return nil }
func (s *stubSIAgentRunRepo) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	return nil, nil
}
func (s *stubSIAgentRunRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	return s.runs, nil
}
func (s *stubSIAgentRunRepo) SumUsageByUser(ctx context.Context, userID int64) (int64, float64, error) {
	return 0, 0, nil
}
func (s *stubSIAgentRunRepo) CountByUser(ctx context.Context, userID int64) (int64, error) {
	return 0, nil
}

type stubSIPromptRepo struct {
	current map[string]*entity.PromptTemplate
	saved   map[string]*entity.PromptTemplate
}

func (s *stubSIPromptRepo) List(ctx context.Context) ([]*entity.PromptTemplate, error) {
	return nil, nil
}
func (s *stubSIPromptRepo) Get(ctx context.Context, key string) (*entity.PromptTemplate, error) {
	return s.current[key], nil
}
func (s *stubSIPromptRepo) Save(ctx context.Context, key, content string) (*entity.PromptTemplate, error) {
	s.saved[key] = &entity.PromptTemplate{Key: key, Content: content}
	return s.saved[key], nil
}
func (s *stubSIPromptRepo) Restore(ctx context.Context, key, content string) (*entity.PromptTemplate, error) {
	return nil, nil
}

func TestService_AnalyzeClassifiesGateFailureAndProposesPrompt(t *testing.T) {
	repo := &stubSIRepo{items: map[string]*entity.SelfImproveSuggestion{}}
	cases := &stubSICaseRepo{c: &entity.DecisionCase{ID: "case-1", Status: entity.CaseStatusFailed}}
	events := &stubSIEventRepo{events: []*entity.MagiEvent{
		{Type: entity.EventToolCallFailed, Payload: []byte(`{"tool_name":"calc","error":"boom"}`)},
		{Type: entity.EventEvidenceGateFailed, Payload: []byte(`{"violations":["missing quantitative"]}`)},
	}}
	agentRuns := &stubSIAgentRunRepo{runs: []*entity.AgentRun{{ID: "run-1", MagiCode: entity.MagiCode("melchior"), Err: "gate failed"}}}
	prompts := &stubSIPromptRepo{
		current: map[string]*entity.PromptTemplate{"agent.workflow_tools": {Key: "agent.workflow_tools", Content: "Use tools."}},
		saved:   map[string]*entity.PromptTemplate{},
	}
	svc := selfimprove.NewService(repo, cases, events, agentRuns, selfimprove.WithPrompts(prompts))

	suggestion, err := svc.Analyze(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if suggestion.Category != entity.SelfImproveGateFailure {
		t.Fatalf("category = %s, want gate_failure", suggestion.Category)
	}
	if suggestion.AgentCode != "melchior" || suggestion.RunID != "run-1" {
		t.Fatalf("agent metadata = %+v", suggestion)
	}
	if suggestion.PromptKey != "agent.workflow_tools" || suggestion.PromptContent == "" ||
		!contains(suggestion.PromptContent, "Gate guidance") {
		t.Fatalf("prompt proposal = %+v", suggestion)
	}
	if repo.items[suggestion.ID] == nil {
		t.Fatal("suggestion must be persisted")
	}

	applied, err := svc.Apply(context.Background(), suggestion.ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied.Status != entity.SelfImproveApplied {
		t.Fatalf("status = %s", applied.Status)
	}
	if prompts.saved["agent.workflow_tools"] == nil {
		t.Fatal("prompt must be written to the registry on apply")
	}
}

func TestService_AnalyzeClassifiesToolError(t *testing.T) {
	repo := &stubSIRepo{items: map[string]*entity.SelfImproveSuggestion{}}
	cases := &stubSICaseRepo{c: &entity.DecisionCase{ID: "case-2", Status: entity.CaseStatusFailed}}
	events := &stubSIEventRepo{events: []*entity.MagiEvent{{Type: entity.EventToolCallFailed, Payload: []byte(`{"tool_name":"db_query"}`)}}}
	svc := selfimprove.NewService(repo, cases, events, &stubSIAgentRunRepo{})
	suggestion, err := svc.Analyze(context.Background(), "case-2")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if suggestion.Category != entity.SelfImproveToolError {
		t.Fatalf("category = %s", suggestion.Category)
	}
	if !contains(suggestion.SuggestedRule, "retry once") {
		t.Fatalf("rule = %q", suggestion.SuggestedRule)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ port.SelfImproveRepository = (*stubSIRepo)(nil)
var _ port.PromptRepository = (*stubSIPromptRepo)(nil)
