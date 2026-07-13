package validation

import (
	"fmt"
	"strings"

	"github.com/bytedance/sonic"
)

// TypedValidator validates JSON against the schema generated from T, then
// unmarshals into T. It is the "validate-then-unmarshal" adapter of ADR-003.
type TypedValidator[T any] struct {
	schema    []byte
	validator Validator
}

// NewTypedValidator generates the JSON Schema for T (cached on the instance)
// and returns a TypedValidator.
func NewTypedValidator[T any](gen SchemaGenerator, v Validator) (*TypedValidator[T], error) {
	var t T
	schema, err := gen.FromStruct(t)
	if err != nil {
		return nil, fmt.Errorf("generate schema for type %T failed: %w", t, err)
	}
	return &TypedValidator[T]{schema: schema, validator: v}, nil
}

// Schema returns the cached JSON Schema bytes for T.
func (tv *TypedValidator[T]) Schema() []byte {
	return tv.schema
}

// ValidateAndUnmarshal validates data against T's schema; if valid, unmarshals
// into a new *T. On validation failure it returns (nil, *ValidationResult)
// without unmarshalling.
// Strips markdown code fences (```json ... ```) before processing.
func (tv *TypedValidator[T]) ValidateAndUnmarshal(data []byte) (*T, *ValidationResult) {
	cleaned := stripMarkdownFences(data)
	res := tv.validator.Validate(tv.schema, cleaned)
	if res == nil || !res.Valid {
		return nil, res
	}
	var t T
	if err := sonic.UnmarshalString(string(cleaned), &t); err != nil {
		return nil, &ValidationResult{
			Valid: false,
			Violations: []Violation{{
				Code:    "UNMARSHAL_FAILED",
				Message: fmt.Sprintf("unmarshal into type %T failed: %v", t, err),
			}},
		}
	}
	return &t, &ValidationResult{Valid: true}
}

// stripMarkdownFences removes leading/trailing ``` fences (with optional language
// tag) from LLM responses that wrap JSON in markdown code blocks.
func stripMarkdownFences(data []byte) []byte {
	s := strings.TrimSpace(string(data))
	// Strip leading ```json or ``` fence
	if strings.HasPrefix(s, "```") {
		nl := strings.Index(s, "\n")
		if nl >= 0 {
			s = s[nl+1:]
		}
	}
	// Strip trailing ``` fence
	if strings.HasSuffix(strings.TrimSpace(s), "```") {
		s = strings.TrimSpace(s)
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return []byte(s)
}
