package selfimprove

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// Service analyzes failed cases into deterministic, human-reviewable
// improvement suggestions. Nothing is applied automatically: the harness
// proposes, an operator approves.
type Service struct {
	repo       port.SelfImproveRepository
	cases      port.CaseRepository
	events     port.EventRepository
	agentRuns  port.AgentRunRepository
	prompts    port.PromptRepository
	model      port.ModelPort
	eventLimit int
	autoApply  bool
	threshold  int
	mode       string // "rule" | "llm" | "hybrid"
}

type Option func(*Service)

// WithPrompts enables applying prompt-content suggestions to the versioned
// prompt registry.
func WithPrompts(p port.PromptRepository) Option {
	return func(s *Service) { s.prompts = p }
}

// WithMode sets the suggestion-generation mode: "rule" (deterministic
// templates), "llm" (generated inline), or "hybrid" (rule first, LLM
// enhancement applied asynchronously).
func WithMode(mode string) Option {
	return func(s *Service) {
		if mode != "" {
			s.mode = mode
		}
	}
}

// WithModel supplies the model builder used to generate contextual
// suggestions in llm/hybrid modes. A nil model falls back to rule templates.
func WithModel(m port.ModelPort) Option {
	return func(s *Service) { s.model = m }
}

// WithAutoApply enables applying recurring suggestions automatically once a
// category reaches the given threshold. Guarded by configuration.
func WithAutoApply(enabled bool, threshold int) Option {
	return func(s *Service) {
		s.autoApply = enabled
		if threshold < 1 {
			threshold = 1
		}
		s.threshold = threshold
	}
}

func NewService(repo port.SelfImproveRepository, cases port.CaseRepository, events port.EventRepository, agentRuns port.AgentRunRepository, opts ...Option) *Service {
	s := &Service{repo: repo, cases: cases, events: events, agentRuns: agentRuns, eventLimit: 200, mode: "rule"}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Analyze inspects a failed case and creates one improvement suggestion.
func (s *Service) Analyze(ctx context.Context, caseID string) (*entity.SelfImproveSuggestion, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("selfimprove: repository not configured")
	}
	c, err := s.cases.Get(ctx, caseID)
	if err != nil || c == nil {
		return nil, fmt.Errorf("selfimprove: case not found")
	}
	var agentCode, runID string
	if s.agentRuns != nil {
		runs, rerr := s.agentRuns.ListByCase(ctx, caseID)
		if rerr == nil {
			for _, r := range runs {
				if r == nil {
					continue
				}
				if r.Err != "" && agentCode == "" {
					agentCode = string(r.MagiCode)
					runID = r.ID
				}
			}
		}
	}
	events, _ := s.events.ListByCase(ctx, caseID)
	if len(events) > s.eventLimit {
		events = events[:s.eventLimit]
	}
	category := s.classify(c, events)
	failure := s.failureSummary(c, events)
	rule := s.suggestRule(category, failure)
	suggestion := &entity.SelfImproveSuggestion{
		ID: "selfimp-" + uuid.NewString(), CaseID: caseID, RunID: runID, AgentCode: agentCode,
		Category: category, Failure: failure, SuggestedRule: rule,
		Status: entity.SelfImproveOpen, CreatedAt: time.Now(),
	}
	if category == entity.SelfImproveGateFailure && s.prompts != nil {
		if current, err := s.prompts.Get(ctx, "agent.workflow_tools"); err == nil && current != nil && current.Content != "" {
			suggestion.PromptKey = "agent.workflow_tools"
			suggestion.PromptContent = current.Content + "\n\n" + rule
		}
	}
	if err := s.repo.Create(ctx, suggestion); err != nil {
		return nil, fmt.Errorf("selfimprove: create suggestion: %w", err)
	}
	if s.mode == "hybrid" && s.model != nil {
		s.enhanceAsync(ctx, suggestion)
	}
	return suggestion, nil
}

// enhanceAsync re-analyzes a suggestion with the model in the background and
// updates its rule once ready. The returned suggestion keeps the deterministic
// rule template and an "analyzing" status until the enhancement lands.
func (s *Service) enhanceAsync(ctx context.Context, suggestion *entity.SelfImproveSuggestion) {
	_ = s.repo.UpdateStatus(ctx, suggestion.ID, entity.SelfImproveAnalyzing)
	go func() {
		rule := s.llmSuggest(suggestion.Category, suggestion.Failure)
		if rule != "" {
			_ = s.repo.UpdateRule(context.Background(), suggestion.ID, rule)
		} else {
			// Enhancement failed; leave the rule template and reopen.
			_ = s.repo.UpdateStatus(context.Background(), suggestion.ID, entity.SelfImproveOpen)
		}
	}()
}

// List returns all suggestions, newest first.
func (s *Service) List(ctx context.Context) ([]*entity.SelfImproveSuggestion, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("selfimprove: repository not configured")
	}
	return s.repo.List(ctx)
}

// Apply marks a suggestion applied. When the suggestion carries proposed
// prompt content, it writes a new version to the prompt registry (still a
// deterministic, versioned, human-approved change).
func (s *Service) Apply(ctx context.Context, id string) (*entity.SelfImproveSuggestion, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("selfimprove: repository not configured")
	}
	suggestion, err := s.repo.Get(ctx, id)
	if err != nil || suggestion == nil {
		return nil, fmt.Errorf("selfimprove: suggestion not found")
	}
	if suggestion.PromptKey != "" && suggestion.PromptContent != "" && s.prompts != nil {
		if _, err := s.prompts.Save(ctx, suggestion.PromptKey, suggestion.PromptContent); err != nil {
			return nil, fmt.Errorf("selfimprove: apply prompt: %w", err)
		}
	}
	if err := s.repo.UpdateStatus(ctx, id, entity.SelfImproveApplied); err != nil {
		return nil, err
	}
	suggestion.Status = entity.SelfImproveApplied
	return suggestion, nil
}

// AutoApply applies the oldest open suggestion of the most frequent category
// once that category has reached the configured threshold. It is a guarded
// automation: suggestions carry prompt content that is written to the
// versioned prompt registry, and repeated failures are required before
// anything changes.
func (s *Service) AutoApply(ctx context.Context) (int, error) {
	if s.repo == nil || !s.autoApply {
		return 0, nil
	}
	all, err := s.repo.List(ctx)
	if err != nil {
		return 0, err
	}
	openByCategory := map[string][]*entity.SelfImproveSuggestion{}
	for _, suggestion := range all {
		if suggestion == nil || suggestion.Status != entity.SelfImproveOpen {
			continue
		}
		openByCategory[suggestion.Category] = append(openByCategory[suggestion.Category], suggestion)
	}
	applied := 0
	for _, suggestions := range openByCategory {
		if len(suggestions) < s.threshold {
			continue
		}
		// Oldest first: pick the earliest suggestion with prompt content.
		for _, suggestion := range suggestions {
			if suggestion.PromptKey == "" || suggestion.PromptContent == "" {
				continue
			}
			if _, err := s.Apply(ctx, suggestion.ID); err != nil {
				continue
			}
			applied++
			break
		}
	}
	return applied, nil
}

func (s *Service) classify(c *entity.DecisionCase, events []*entity.MagiEvent) string {
	switch c.Status {
	case entity.CaseStatusTimedOut:
		return entity.SelfImproveTimeout
	case entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		return entity.SelfImproveGateFailure
	case entity.CaseStatusFailed:
		// fall through to event-based classification
	default:
		return entity.SelfImproveOther
	}
	for _, e := range events {
		if e == nil {
			continue
		}
		if e.Type == entity.EventEvidenceGateFailed {
			return entity.SelfImproveGateFailure
		}
	}
	for _, e := range events {
		if e == nil {
			continue
		}
		switch e.Type {
		case entity.EventModelRequested:
			return entity.SelfImproveModelError
		}
	}
	for _, e := range events {
		if e == nil {
			continue
		}
		if e.Type == entity.EventToolCallFailed {
			return entity.SelfImproveToolError
		}
	}
	return entity.SelfImproveOther
}

func (s *Service) failureSummary(c *entity.DecisionCase, events []*entity.MagiEvent) string {
	var parts []string
	for _, e := range events {
		if e == nil {
			continue
		}
		switch e.Type {
		case entity.EventCaseFailed, entity.EventToolCallFailed, entity.EventEvidenceGateFailed, entity.EventMemoryRetrievalFailed:
			msg := strings.TrimSpace(string(e.Payload))
			if msg != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", e.Type, msg))
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return "case status: " + string(c.Status)
}

func (s *Service) suggestRule(category, failure string) string {
	switch s.mode {
	case "llm":
		if rule := s.llmSuggest(category, failure); rule != "" {
			return rule
		}
	}
	return ruleTemplate(category, failure)
}

// llmSuggest generates a contextual improvement suggestion via the configured
// model. On any failure (no model, build error, generation error) it returns
// "" so callers fall back to the deterministic rule template.
func (s *Service) llmSuggest(category, failure string) string {
	if s.model == nil {
		return ""
	}
	prompt := "You are the MAGI harness self-improvement engine. The failure category is " + category +
		". Details: " + truncate(failure, 1500) +
		". Suggest ONE concrete, actionable improvement rule (at most 300 characters)."
	cm, err := s.model.Build(context.Background(), s.modelRef())
	if err != nil {
		return ""
	}
	resp, err := cm.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("You are the MAGI harness self-improvement engine. Respond with a single concise improvement rule."),
		schema.UserMessage(prompt),
	})
	if err != nil || resp == nil {
		return ""
	}
	rule := strings.TrimSpace(resp.Content)
	if rule == "" {
		return ""
	}
	return rule
}

// modelRef returns an empty ModelRef; callers may override via wiring to the
// default global model when no per-instance ref is configured.
func (s *Service) modelRef() entity.ModelRef {
	return entity.ModelRef{}
}

// ruleTemplate returns the deterministic, category-based suggestion template.
func ruleTemplate(category, failure string) string {
	switch category {
	case entity.SelfImproveGateFailure:
		return "Gate guidance: before voting, the EvidenceSummary must satisfy every required evidence type and minimum count; gather additional evidence instead of fabricating EV-IDs."
	case entity.SelfImproveToolError:
		return "Tool guidance: when a tool fails, retry once with corrected arguments or switch to an alternative tool; never fabricate the tool result."
	case entity.SelfImproveValidation:
		return "Format guidance: output the required structured JSON; the system lints it against the expected schema automatically. You may use the check_output tool with 'rules' for extra field constraints."
	case entity.SelfImproveTimeout:
		return "Budget guidance: keep reasoning compact so the run completes within the configured timeout; reduce repeated tool calls."
	case entity.SelfImproveModelError:
		return "Model guidance: the provider call failed; consider a fallback provider or retrying with a shorter request."
	default:
		if failure == "" {
			return "Review the failed case event log before the next run."
		}
		return "Review failure: " + truncate(failure, 200)
	}
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
