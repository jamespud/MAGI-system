package validation

import (
	"fmt"

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
func (tv *TypedValidator[T]) ValidateAndUnmarshal(data []byte) (*T, *ValidationResult) {
	res := tv.validator.Validate(tv.schema, data)
	if res == nil || !res.Valid {
		return nil, res
	}
	var t T
	if err := sonic.UnmarshalString(string(data), &t); err != nil {
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
