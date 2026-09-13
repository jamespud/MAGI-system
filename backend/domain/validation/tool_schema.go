package validation

import (
	"encoding/json"
	"fmt"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// NormalizeToolSchema prepares a provider-supplied tool argument schema for the
// model request. Providers expect function parameters to be a top-level object
// schema; mcp-go emits `"type":""` whenever the MCP server omits `type`, which
// strict providers reject with a 400 (see the stock-mcp incident).
//
// Note: NormalizeJSONSchema round-trips through map[string]any, so numeric
// literals in `default`/`enum` lose their original JSON number type. That is
// pre-existing behaviour and is deliberately not widened here.
func NormalizeToolSchema(schema []byte) ([]byte, error) {
	normalized, err := NormalizeJSONSchema(schema)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal normalized tool schema: %w", err)
	}
	if t, _ := doc["type"].(string); t == "" {
		doc["type"] = "object"
	}
	if _, ok := doc["properties"]; !ok {
		doc["properties"] = map[string]any{}
	}
	return json.Marshal(doc)
}

// CompileSchema reports whether schema is a well-formed JSON Schema document
// under the dialect the runtime validator uses. A schema that fails here would
// also be rejected (or silently misinterpreted) by the model provider.
func CompileSchema(schema []byte) error {
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return fmt.Errorf("schema is not valid JSON: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	const url = "magi://tool-schema/probe.json"
	if err := compiler.AddResource(url, doc); err != nil {
		return fmt.Errorf("schema resource rejected: %w", err)
	}
	if _, err := compiler.Compile(url); err != nil {
		return fmt.Errorf("schema does not compile: %w", err)
	}
	return nil
}
