package magi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// DelegateToolName is the built-in sub-investigation delegation tool.
const DelegateToolName = "delegate"

// delegateArgsSchema is the JSON Schema for delegate arguments.
const delegateArgsSchema = `{"type":"object","properties":{"question":{"type":"string","description":"sub-question to investigate independently"},"background":{"type":"string"}},"required":["question"],"additionalProperties":false}`

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

// DelegateToolExecutor runs a sub-investigation and returns its evidence as
// structured output for the calling agent to cite.
type DelegateToolExecutor struct {
	investigator SubInvestigator
}

// NewDelegateToolExecutor wraps a sub-investigator behind the local tool
// contract.
func NewDelegateToolExecutor(investigator SubInvestigator) (port.ToolExecutorPort, error) {
	if investigator == nil {
		return nil, fmt.Errorf("delegate: sub-investigator is required")
	}
	return &DelegateToolExecutor{investigator: investigator}, nil
}

func (e *DelegateToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	var args struct {
		Question   string `json:"question"`
		Background string `json:"background,omitempty"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("delegate: parse args: %w", err)
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

var _ port.ToolExecutorPort = (*DelegateToolExecutor)(nil)
