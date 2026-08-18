package magi_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

func TestFeedbackToolExecutor_RunsSchemaAndConstraints(t *testing.T) {
	reg := metrics.New()
	sensors := runtime.NewCompositeFeedbackSensor(
		runtime.NewSchemaFeedbackSensor(validation.NewJSONSchemaValidator()),
		runtime.NewConstraintFeedbackSensor(),
	)
	exec := magi.NewFeedbackToolExecutor(sensors, reg)
	args := `{"payload":{"decision":"maybe","confidence":0.5},"schema":"{\"type\":\"object\",\"properties\":{\"decision\":{\"type\":\"string\",\"enum\":[\"approve\",\"reject\"]}},\"required\":[\"decision\"]}","rules":[{"field":"confidence","op":"gte","value":0.9}]}`
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FeedbackToolName, ArgumentsJSON: args,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		OK         bool                        `json:"ok"`
		Violations []runtime.FeedbackViolation `json:"violations"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.OK {
		t.Fatal("invalid payload must not pass")
	}
	if len(out.Violations) < 2 {
		t.Fatalf("expected schema + constraint violations, got %+v", out.Violations)
	}
	if reg.FeedbackViolations.Load() != int64(len(out.Violations)) {
		t.Fatalf("violation metric = %d, want %d", reg.FeedbackViolations.Load(), len(out.Violations))
	}
}

func TestFeedbackToolExecutor_RequiresPayloadOrChecks(t *testing.T) {
	exec := magi.NewFeedbackToolExecutor(runtime.NewCompositeFeedbackSensor(runtime.NewConstraintFeedbackSensor()), nil)
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FeedbackToolName, ArgumentsJSON: `{}`,
	}); err == nil || !strings.Contains(err.Error(), "payload is required") {
		t.Fatalf("expected payload error, got %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.FeedbackToolName, ArgumentsJSON: `{"payload":{"x":1}}`,
	}); err == nil || !strings.Contains(err.Error(), "schema or constraint rules") {
		t.Fatalf("expected checks error, got %v", err)
	}
}

func TestFeedbackToolExecutor_PassesCleanPayload(t *testing.T) {
	exec := magi.NewFeedbackToolExecutor(runtime.NewCompositeFeedbackSensor(runtime.NewConstraintFeedbackSensor()), nil)
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName:      magi.FeedbackToolName,
		ArgumentsJSON: `{"payload":{"confidence":0.95},"rules":[{"field":"confidence","op":"gte","value":0.9}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res.Output, `"ok":true`) {
		t.Fatalf("clean payload must pass: %s", res.Output)
	}
}

var _ port.ToolExecutorPort = (*magi.FeedbackToolExecutor)(nil)
