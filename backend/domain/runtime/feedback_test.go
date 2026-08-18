package runtime_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

func TestSchemaFeedbackSensor_LintsPayload(t *testing.T) {
	sensor := runtime.NewSchemaFeedbackSensor(validation.NewJSONSchemaValidator())
	schema := []byte(`{"type":"object","properties":{"decision":{"type":"string","enum":["approve","reject"]}},"required":["decision"]}`)

	ok, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind: runtime.FeedbackCheckSchema, Payload: map[string]any{"decision": "approve"}, Schema: schema,
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(ok) != 0 {
		t.Fatalf("valid payload must pass: %+v", ok)
	}

	violations, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind: runtime.FeedbackCheckSchema, Payload: map[string]any{"decision": "maybe"}, Schema: schema,
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(violations) == 0 {
		t.Fatal("invalid enum must produce violations")
	}
}

func TestConstraintFeedbackSensor_AppliesRules(t *testing.T) {
	sensor := runtime.NewConstraintFeedbackSensor()
	violations, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind:    runtime.FeedbackCheckConstraints,
		Payload: map[string]any{"decision": "approve", "confidence": 0.8, "note": "rollback ready"},
		Rules: []runtime.ConstraintRule{
			{Field: "decision", Op: "eq", Value: "approve"},
			{Field: "confidence", Op: "gte", Value: 0.9},
			{Field: "note", Op: "contains", Value: "rollback"},
		},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(violations) != 1 || violations[0].Field != "confidence" {
		t.Fatalf("expected only confidence violation, got %+v", violations)
	}

	missing, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind: runtime.FeedbackCheckConstraints, Payload: map[string]any{},
		Rules: []runtime.ConstraintRule{{Field: "decision", Op: "eq", Value: "approve"}},
	})
	if err != nil || len(missing) != 1 || missing[0].Field != "decision" {
		t.Fatalf("missing field violation = %+v err=%v", missing, err)
	}
}

func TestCompositeFeedbackSensor_Aggregates(t *testing.T) {
	sensor := runtime.NewCompositeFeedbackSensor(
		runtime.NewSchemaFeedbackSensor(validation.NewJSONSchemaValidator()),
		runtime.NewConstraintFeedbackSensor(),
	)
	schema := []byte(`{"type":"object","properties":{"decision":{"type":"string","enum":["approve"]}},"required":["decision"]}`)
	violations, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind: runtime.FeedbackCheckSchema, Payload: map[string]any{"decision": "reject"}, Schema: schema,
	})
	if err != nil {
		t.Fatalf("schema check: %v", err)
	}
	constr, err := sensor.Check(context.Background(), runtime.FeedbackCheck{
		Kind: runtime.FeedbackCheckConstraints, Payload: map[string]any{"confidence": 0.5},
		Rules: []runtime.ConstraintRule{{Field: "confidence", Op: "gte", Value: 0.9}},
	})
	if err != nil {
		t.Fatalf("constraint check: %v", err)
	}
	if len(violations) == 0 || len(constr) == 0 {
		t.Fatalf("both sensors must report: schema=%+v constraints=%+v", violations, constr)
	}
}
