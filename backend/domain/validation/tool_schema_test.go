package validation

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolSchema(t *testing.T) {
	// mcp-go emits `"type":""` for a server tool that omits the top-level type.
	got, err := NormalizeToolSchema([]byte(`{"type":"","properties":{"symbol":{"type":"string"}}}`))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal normalized: %v", err)
	}
	if doc["type"] != "object" {
		t.Fatalf("top-level type = %v, want object", doc["type"])
	}
	if _, ok := doc["properties"].(map[string]any); !ok {
		t.Fatalf("properties = %#v, want map", doc["properties"])
	}

	// A schema without type/properties still comes back provider-shaped.
	got, err = NormalizeToolSchema([]byte(`{}`))
	if err != nil {
		t.Fatalf("normalize empty: %v", err)
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	if doc["type"] != "object" {
		t.Fatalf("empty schema type = %v, want object", doc["type"])
	}
}

func TestCompileSchema(t *testing.T) {
	valid := []byte(`{"type":"object","properties":{"s":{"type":"string"}},"required":["s"],"additionalProperties":false}`)
	if err := CompileSchema(valid); err != nil {
		t.Fatalf("valid schema rejected: %v", err)
	}
	union := []byte(`{"anyOf":[{"type":"string"},{"type":"object","properties":{"n":{"type":"string"}},"required":["n"],"additionalProperties":false}]}`)
	if err := CompileSchema(union); err != nil {
		t.Fatalf("pure union rejected: %v", err)
	}
	emptyType := []byte(`{"type":"","properties":{"s":{"type":"string"}}}`)
	if err := CompileSchema(emptyType); err == nil {
		t.Fatal("empty top-level type accepted, want metaschema error")
	}
	if err := CompileSchema([]byte(`{"type":`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
