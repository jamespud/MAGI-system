package magi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// DelegateToolName is the built-in sub-investigation delegation tool.
const DelegateToolName = "delegate"

// delegateArgsSchema is the JSON Schema for delegate arguments.
const delegateArgsSchema = `{"type":"object","properties":{"question":{"type":"string","description":"sub-question to investigate independently"},"background":{"type":"string"},"questions":{"type":"array","items":{"type":"object","properties":{"question":{"type":"string"},"background":{"type":"string"}},"required":["question"],"additionalProperties":false},"description":"multiple sub-questions to investigate in parallel"}},"anyOf":[{"required":["question"]},{"required":["questions"]}],"additionalProperties":false}`

// DelegateResult is the evidence bundle returned by a sub-investigation.
type DelegateResult struct {
	Question string
	Status   string
	Evidence []map[string]any
}

// SubInvestigator runs an independent sub-investigation (a dynamically
// derived subagent).
type SubInvestigator interface {
	Investigate(ctx context.Context, question, background string) (*DelegateResult, error)
}

// LoopSubInvestigator derives a subagent from the main AgentLoop: it runs a
// fresh investigation of the sub-question and returns the collected evidence.
type LoopSubInvestigator struct {
	loop runtime.MagiRuntime
	cfg  *entity.MagiConfig
}

// NewLoopSubInvestigator wires a sub-investigator backed by the shared agent
// loop and one role configuration.
func NewLoopSubInvestigator(loop runtime.MagiRuntime, cfg *entity.MagiConfig) (*LoopSubInvestigator, error) {
	if loop == nil {
		return nil, fmt.Errorf("delegate: agent loop is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("delegate: role config is required")
	}
	return &LoopSubInvestigator{loop: loop, cfg: cfg}, nil
}

func (s *LoopSubInvestigator) Investigate(ctx context.Context, question, background string) (*DelegateResult, error) {
	actx := &runtime.AgentContext{
		Task: entity.DecisionTask{CanonicalQuestion: question, Background: background},
	}
	res, err := s.loop.Run(ctx, s.cfg, actx)
	if err != nil {
		return nil, fmt.Errorf("delegate sub-investigation: %w", err)
	}
	out := &DelegateResult{Question: question}
	if res != nil {
		out.Status = string(res.Status)
		if res.Ledger != nil {
			for _, ev := range res.Ledger.List() {
				source := ""
				if ev.SourceURI != nil {
					source = *ev.SourceURI
				}
				out.Evidence = append(out.Evidence, map[string]any{
					"id": ev.ID, "tool": ev.ToolName, "source": source,
					"observation": ev.Observation, "reliability": ev.Reliability.Final,
				})
			}
		}
	}
	return out, nil
}

// DelegateToolConfig configures the delegate tool.
type DelegateToolConfig struct {
	MaxParallel int
}

// DelegateToolExecutor runs a sub-investigation and returns its evidence as
// structured output for the calling agent to cite.
type DelegateToolExecutor struct {
	investigator SubInvestigator
	maxParallel  int
}

// NewDelegateToolExecutor wraps a sub-investigator behind the local tool
// contract.
func NewDelegateToolExecutor(investigator SubInvestigator, cfg DelegateToolConfig) (port.ToolExecutorPort, error) {
	if investigator == nil {
		return nil, fmt.Errorf("delegate: sub-investigator is required")
	}
	mp := cfg.MaxParallel
	if mp <= 0 {
		mp = 4
	}
	return &DelegateToolExecutor{investigator: investigator, maxParallel: mp}, nil
}

func (e *DelegateToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Question   string `json:"question"`
		Background string `json:"background,omitempty"`
		Questions  []struct {
			Question   string `json:"question"`
			Background string `json:"background,omitempty"`
		} `json:"questions,omitempty"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("delegate: parse args: %w", err)
	}
	if len(args.Questions) > 0 {
		return e.executeParallel(ctx, args.Questions)
	}
	if args.Question == "" {
		return nil, fmt.Errorf("delegate: question is required")
	}
	result, err := e.investigator.Investigate(ctx, args.Question, args.Background)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"question": result.Question, "status": result.Status,
		"evidence_count": len(result.Evidence), "evidence": result.Evidence,
	}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

const delegateParallelLimit = 4

// executeParallel runs multiple sub-investigations concurrently (bounded)
// and merges their evidence into one structured result.
func (e *DelegateToolExecutor) executeParallel(ctx context.Context, questions []struct {
	Question   string `json:"question"`
	Background string `json:"background,omitempty"`
}) (*port.ToolExecutionResult, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("delegate: at least one question is required")
	}
	results := make([]*DelegateResult, len(questions))
	errs := make([]error, len(questions))
	limit := e.maxParallel
	if len(questions) < limit {
		limit = len(questions)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, item := range questions {
		if item.Question == "" {
			return nil, fmt.Errorf("delegate: question %d is empty", i)
		}
		wg.Add(1)
		go func(idx int, question, background string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx], errs[idx] = e.investigator.Investigate(ctx, question, background)
		}(i, item.Question, item.Background)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("delegate parallel: %w", err)
		}
	}
	out := map[string]any{
		"results": results, "evidence_count": totalEvidence(results),
	}
	raw, _ := json.Marshal(out)
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

func totalEvidence(results []*DelegateResult) int {
	total := 0
	for _, r := range results {
		if r != nil {
			total += len(r.Evidence)
		}
	}
	return total
}

var _ port.ToolExecutorPort = (*DelegateToolExecutor)(nil)
