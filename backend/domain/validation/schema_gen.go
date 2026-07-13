package validation

import (
	"encoding/json"
	"fmt"

	einojsonschema "github.com/eino-contrib/jsonschema"
)

// SchemaGenerator generates a JSON Schema (bytes) from a Go struct type.
// This is the struct-first developer-experience path of ADR-003: the struct is
// only a typed adapter; the generated JSON Schema is the runtime contract.
type SchemaGenerator interface {
	// FromStruct generates a JSON Schema for the type of v. v should be a zero
	// value of the target struct type.
	FromStruct(v any) ([]byte, error)
}

type reflectSchemaGenerator struct{}

// NewReflectSchemaGenerator returns a SchemaGenerator backed by
// eino-contrib/jsonschema (Reflect).
func NewReflectSchemaGenerator() SchemaGenerator {
	return &reflectSchemaGenerator{}
}

func (g *reflectSchemaGenerator) FromStruct(v any) ([]byte, error) {
	sch := einojsonschema.Reflect(v)
	if sch == nil {
		return nil, fmt.Errorf("reflect schema returned nil for type %T", v)
	}
	b, err := json.Marshal(sch)
	if err != nil {
		return nil, fmt.Errorf("marshal reflected schema for type %T failed: %w", v, err)
	}
	return NormalizeJSONSchema(b)
}

// NormalizeJSONSchema removes empty "type":"" fields that eino-contrib emits at
// the schema root (alongside $ref/$defs); they violate the 2020-12 meta-schema.
// It recurses into $defs/properties/items so nested schemas are cleaned too.
func NormalizeJSONSchema(b []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshal schema for normalization failed: %w", err)
	}
	removeEmptyType(m)
	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("remarshal normalized schema failed: %w", err)
	}
	return out, nil
}

func removeEmptyType(m map[string]any) {
	if t, ok := m["type"]; ok {
		if s, ok := t.(string); ok && s == "" {
			delete(m, "type")
		}
	}
	for _, v := range m {
		switch vv := v.(type) {
		case map[string]any:
			removeEmptyType(vv)
		case []any:
			for _, e := range vv {
				if em, ok := e.(map[string]any); ok {
					removeEmptyType(em)
				}
			}
		}
	}
}
