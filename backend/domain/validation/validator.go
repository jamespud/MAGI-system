package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates a JSON value against a JSON Schema (ADR-003 unified IR).
type Validator interface {
	// Validate compiles schema (cached) and validates value against it.
	// A non-nil ValidationResult with Valid==false means validation failed;
	// the returned value is never nil.
	Validate(schema []byte, value []byte) *ValidationResult
}

type jsonSchemaValidator struct {
	mu       sync.RWMutex
	compiled map[string]*jsonschema.Schema
}

// NewJSONSchemaValidator returns a Validator backed by santhosh-tekuri/jsonschema/v6.
func NewJSONSchemaValidator() Validator {
	return &jsonSchemaValidator{compiled: make(map[string]*jsonschema.Schema)}
}

func (v *jsonSchemaValidator) Validate(schema []byte, value []byte) *ValidationResult {
	sch, res := v.compile(schema)
	if res != nil {
		return res
	}

	var val any
	if err := sonic.Unmarshal(value, &val); err != nil {
		return &ValidationResult{
			Valid: false,
			Violations: []Violation{{
				Code:    "INVALID_JSON",
				Message: fmt.Sprintf("unmarshal value failed: %v", err),
			}},
		}
	}

	if err := sch.Validate(val); err != nil {
		return &ValidationResult{Valid: false, Violations: extractViolations(err)}
	}
	return &ValidationResult{Valid: true}
}

// compile returns the compiled schema, cached by schema content hash. If
// compilation fails, it returns a ValidationResult describing the failure.
func (v *jsonSchemaValidator) compile(schema []byte) (*jsonschema.Schema, *ValidationResult) {
	key := hashKey(schema)

	v.mu.RLock()
	if sch, ok := v.compiled[key]; ok {
		v.mu.RUnlock()
		return sch, nil
	}
	v.mu.RUnlock()

	v.mu.Lock()
	defer v.mu.Unlock()
	if sch, ok := v.compiled[key]; ok {
		return sch, nil
	}

	// Parse schema bytes into a generic doc; AddResource accepts a parsed
	// value, which avoids depending on bytes/string decoding behaviour.
	var doc any
	if err := sonic.Unmarshal(schema, &doc); err != nil {
		return nil, &ValidationResult{
			Valid: false,
			Violations: []Violation{{
				Code:    "SCHEMA_INVALID_JSON",
				Message: fmt.Sprintf("unmarshal schema failed: %v", err),
			}},
		}
	}

	compiler := jsonschema.NewCompiler()
	url := "magi://schema/" + key + ".json"
	if err := compiler.AddResource(url, doc); err != nil {
		return nil, &ValidationResult{
			Valid: false,
			Violations: []Violation{{
				Code:    "SCHEMA_ADD_RESOURCE_FAILED",
				Message: fmt.Sprintf("add schema resource failed: %v", err),
			}},
		}
	}

	sch, err := compiler.Compile(url)
	if err != nil {
		return nil, &ValidationResult{
			Valid: false,
			Violations: []Violation{{
				Code:    "SCHEMA_COMPILE_FAILED",
				Message: fmt.Sprintf("compile schema failed: %v", err),
			}},
		}
	}

	v.compiled[key] = sch
	return sch, nil
}

func hashKey(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func extractViolations(err error) []Violation {
	var ve *jsonschema.ValidationError
	if errors.As(err, &ve) {
		return walkViolations(ve)
	}
	return []Violation{{Code: "VALIDATION_FAILED", Message: err.Error()}}
}

// walkViolations collects leaf violations from the ValidationError tree.
func walkViolations(ve *jsonschema.ValidationError) []Violation {
	out := make([]Violation, 0, 1)
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if e == nil {
			return
		}
		if len(e.Causes) == 0 {
			out = append(out, Violation{
				Code:    kindCode(e.ErrorKind),
				Message: kindMessage(e.ErrorKind, e.InstanceLocation),
				Field:   joinPath(e.InstanceLocation),
			})
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(out) == 0 {
		out = append(out, Violation{Code: "VALIDATION_FAILED", Message: ve.GoString()})
	}
	return out
}

func kindCode(k jsonschema.ErrorKind) string {
	if k == nil {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%T", k)
}

func kindMessage(k jsonschema.ErrorKind, loc []string) string {
	if k == nil {
		return "validation failed at " + joinPath(loc)
	}
	return fmt.Sprintf("value at %s invalid: %T", joinPath(loc), k)
}

func joinPath(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "."
		}
		out += p
	}
	return out
}
