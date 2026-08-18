package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jamespud/magi/backend/domain/validation"
)

// FeedbackViolation is one deterministic check failure fed back to the model
// for self-correction.
type FeedbackViolation struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// FeedbackCheckKind identifies the deterministic sensor to run.
type FeedbackCheckKind string

const (
	FeedbackCheckSchema      FeedbackCheckKind = "schema"
	FeedbackCheckConstraints FeedbackCheckKind = "constraints"
)

// ConstraintRule is a deterministic predicate over one payload field.
type ConstraintRule struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq | ne | gt | gte | lt | lte | contains
	Value any    `json:"value"`
}

// FeedbackCheck is one deterministic check over a model-produced payload.
type FeedbackCheck struct {
	Kind    FeedbackCheckKind
	Payload any
	Schema  []byte
	Rules   []ConstraintRule
}

// FeedbackSensor runs deterministic checks and returns violations. Results
// are fed back to the model so it can self-correct before human review.
type FeedbackSensor interface {
	Check(ctx context.Context, check FeedbackCheck) ([]FeedbackViolation, error)
}

// SchemaFeedbackSensor validates a payload against a JSON Schema using the
// same deterministic validator used for tool arguments.
type SchemaFeedbackSensor struct {
	validator validation.Validator
}

func NewSchemaFeedbackSensor(v validation.Validator) *SchemaFeedbackSensor {
	return &SchemaFeedbackSensor{validator: v}
}

func (s *SchemaFeedbackSensor) Check(ctx context.Context, check FeedbackCheck) ([]FeedbackViolation, error) {
	if check.Kind != FeedbackCheckSchema || s.validator == nil || len(check.Schema) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(check.Payload)
	if err != nil {
		return nil, fmt.Errorf("feedback schema: marshal payload: %w", err)
	}
	res := s.validator.Validate(check.Schema, raw)
	if res == nil {
		return nil, nil
	}
	if !res.Valid && len(res.Violations) > 0 && strings.HasPrefix(res.Violations[0].Code, "SCHEMA_") {
		return nil, fmt.Errorf("feedback schema: %s", res.Error())
	}
	if res.Valid {
		return nil, nil
	}
	violations := make([]FeedbackViolation, 0, len(res.Violations))
	for _, v := range res.Violations {
		violations = append(violations, FeedbackViolation{Field: v.Field, Message: v.Message})
	}
	return violations, nil
}

// ConstraintFeedbackSensor applies deterministic field predicates.
type ConstraintFeedbackSensor struct{}

func NewConstraintFeedbackSensor() *ConstraintFeedbackSensor {
	return &ConstraintFeedbackSensor{}
}

func (s *ConstraintFeedbackSensor) Check(ctx context.Context, check FeedbackCheck) ([]FeedbackViolation, error) {
	if check.Kind != FeedbackCheckConstraints || len(check.Rules) == 0 {
		return nil, nil
	}
	payload, ok := check.Payload.(map[string]any)
	if !ok {
		// Allow arbitrary JSON objects produced by the model.
		var decoded map[string]any
		if raw, err := json.Marshal(check.Payload); err == nil {
			_ = json.Unmarshal(raw, &decoded)
			payload = decoded
		}
	}
	var violations []FeedbackViolation
	for _, rule := range check.Rules {
		actual, found := lookupField(payload, rule.Field)
		message := evalRule(rule, actual, found)
		if message != "" {
			violations = append(violations, FeedbackViolation{Field: rule.Field, Message: message})
		}
	}
	return violations, nil
}

func lookupField(payload map[string]any, field string) (any, bool) {
	if payload == nil {
		return nil, false
	}
	if value, ok := payload[field]; ok {
		return value, true
	}
	// Dotted paths resolve nested maps (e.g. "decision.conditions").
	parts := strings.Split(field, ".")
	current := any(payload)
	for _, part := range parts {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = node[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func evalRule(rule ConstraintRule, actual any, found bool) string {
	if !found {
		return fmt.Sprintf("field %q is missing", rule.Field)
	}
	switch rule.Op {
	case "eq":
		if !valuesEqual(actual, rule.Value) {
			return fmt.Sprintf("field %q must equal %v (got %v)", rule.Field, rule.Value, actual)
		}
	case "ne":
		if valuesEqual(actual, rule.Value) {
			return fmt.Sprintf("field %q must not equal %v", rule.Field, rule.Value)
		}
	case "gt", "gte", "lt", "lte":
		av, aerr := toFloat64(actual)
		bv, berr := toFloat64(rule.Value)
		if aerr != nil || berr != nil {
			return fmt.Sprintf("field %q requires numeric values for %s", rule.Field, rule.Op)
		}
		ok := false
		switch rule.Op {
		case "gt":
			ok = av > bv
		case "gte":
			ok = av >= bv
		case "lt":
			ok = av < bv
		case "lte":
			ok = av <= bv
		}
		if !ok {
			return fmt.Sprintf("field %q must satisfy %s %v (got %v)", rule.Field, rule.Op, bv, av)
		}
	case "contains":
		text, ok := actual.(string)
		want, wok := rule.Value.(string)
		if !ok || !wok || !strings.Contains(text, want) {
			return fmt.Sprintf("field %q must contain %q", rule.Field, fmt.Sprint(rule.Value))
		}
	default:
		return fmt.Sprintf("unsupported constraint operator %q", rule.Op)
	}
	return ""
}

func valuesEqual(a, b any) bool {
	af, aerr := toFloat64(a)
	bf, berr := toFloat64(b)
	if aerr == nil && berr == nil {
		return af == bf
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("not numeric")
	}
}

// CompositeFeedbackSensor runs every registered sensor and aggregates
// violations.
type CompositeFeedbackSensor struct {
	sensors []FeedbackSensor
}

func NewCompositeFeedbackSensor(sensors ...FeedbackSensor) *CompositeFeedbackSensor {
	return &CompositeFeedbackSensor{sensors: sensors}
}

func (s *CompositeFeedbackSensor) Check(ctx context.Context, check FeedbackCheck) ([]FeedbackViolation, error) {
	var out []FeedbackViolation
	for _, sensor := range s.sensors {
		violations, err := sensor.Check(ctx, check)
		if err != nil {
			return nil, err
		}
		out = append(out, violations...)
	}
	return out, nil
}
