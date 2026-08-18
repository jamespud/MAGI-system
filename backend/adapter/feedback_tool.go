package magi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// FeedbackToolName is the local deterministic check tool.
const FeedbackToolName = "check_output"

// feedbackArgsSchema is the JSON Schema for check_output arguments.
const feedbackArgsSchema = `{"type":"object","properties":{"payload":{},"schema":{"type":"string","description":"optional JSON Schema to lint the payload against"},"rules":{"type":"array","items":{"type":"object","properties":{"field":{"type":"string"},"op":{"type":"string","enum":["eq","ne","gt","gte","lt","lte","contains"]},"value":{}},"required":["field","op"],"additionalProperties":false}},"required":["payload"],"additionalProperties":false}`

// FeedbackToolExecutor runs deterministic feedback sensors on the model's own
// output and returns violations so the loop feeds them back for self-correction.
type FeedbackToolExecutor struct {
	sensors runtime.FeedbackSensor
	metrics *metrics.Registry
}

// NewFeedbackToolExecutor wraps one or more deterministic sensors behind the
// local tool contract.
func NewFeedbackToolExecutor(sensors runtime.FeedbackSensor, reg *metrics.Registry) port.ToolExecutorPort {
	return &FeedbackToolExecutor{sensors: sensors, metrics: reg}
}

func (e *FeedbackToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	if e.sensors == nil {
		return nil, fmt.Errorf("check_output: feedback sensors are not configured")
	}
	var args struct {
		Payload any                      `json:"payload"`
		Schema  string                   `json:"schema,omitempty"`
		Rules   []runtime.ConstraintRule `json:"rules,omitempty"`
	}
	if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("check_output: parse args: %w", err)
	}
	if args.Payload == nil {
		return nil, fmt.Errorf("check_output: payload is required")
	}
	var checks []runtime.FeedbackCheck
	if args.Schema != "" {
		checks = append(checks, runtime.FeedbackCheck{
			Kind: runtime.FeedbackCheckSchema, Payload: args.Payload, Schema: []byte(args.Schema),
		})
	}
	if len(args.Rules) > 0 {
		checks = append(checks, runtime.FeedbackCheck{
			Kind: runtime.FeedbackCheckConstraints, Payload: args.Payload, Rules: args.Rules,
		})
	}
	if len(checks) == 0 {
		return nil, fmt.Errorf("check_output: provide a schema or constraint rules")
	}
	var all []runtime.FeedbackViolation
	for _, check := range checks {
		violations, err := e.sensors.Check(ctx, check)
		if err != nil {
			return nil, fmt.Errorf("check_output: %w", err)
		}
		all = append(all, violations...)
	}
	out := map[string]any{
		"ok":         len(all) == 0,
		"violations": all,
	}
	if e.metrics != nil && len(all) > 0 {
		e.metrics.AddFeedbackViolations(int64(len(all)))
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("check_output: encode result: %w", err)
	}
	return &port.ToolExecutionResult{Output: string(raw), Structured: out}, nil
}

var _ port.ToolExecutorPort = (*FeedbackToolExecutor)(nil)
