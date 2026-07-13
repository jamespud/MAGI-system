



// Package validation implements the unified schema-first Runtime Validation
// IR. JSON Schema is the single validation contract for both plugin tools
// (schema derived from OpenAPI3) and local Go tools (schema generated from
// a struct). go-playground/validator is intentionally NOT the runtime source
// of truth.
package validation

import (
	"fmt"
	"strings"
)

// Violation is a single validation failure.
type Violation struct {
	Code     string
	Message  string
	Field    string
	Expected string
	Actual   string
}

// ValidationResult aggregates the outcome of a validation. It implements error
// so callers can return it directly when invalid.
type ValidationResult struct {
	Valid      bool
	Violations []Violation
}

// Error implements error.
func (r *ValidationResult) Error() string {
	if r == nil || r.Valid {
		return ""
	}
	msgs := make([]string, 0, len(r.Violations))
	for _, v := range r.Violations {
		msgs = append(msgs, fmt.Sprintf("[%s] field=%q: %s", v.Code, v.Field, v.Message))
	}
	return strings.Join(msgs, "; ")
}
