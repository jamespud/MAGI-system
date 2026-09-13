package magi

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// A self-referencing $ref resolves into a pointer cycle. The converter used to
// recurse into it until the process died with "fatal error: stack overflow".
func TestOpenAPISchema_RecursiveRefTerminates(t *testing.T) {
	node := &openapi3.Schema{Type: "object"}
	node.Properties = openapi3.Schemas{
		"name":  &openapi3.SchemaRef{Value: &openapi3.Schema{Type: "string"}},
		"child": &openapi3.SchemaRef{Value: node}, // the cycle
	}

	out := openAPISchema(node)

	if out["type"] != "object" {
		t.Fatalf("type = %v, want object", out["type"])
	}
	props, ok := out["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v, want map", out["properties"])
	}
	if _, ok := props["name"]; !ok {
		t.Fatalf("non-cyclic sibling property lost: %#v", props)
	}
	child, ok := props["child"].(map[string]any)
	if !ok || child["type"] != "object" {
		t.Fatalf("child = %#v, want degraded object schema", props["child"])
	}
}

// A deep but acyclic schema must be bounded too.
func TestOpenAPISchema_DepthBounded(t *testing.T) {
	root := &openapi3.Schema{Type: "object"}
	cur := root
	for i := 0; i < openAPISchemaMaxDepth+5; i++ {
		next := &openapi3.Schema{Type: "object"}
		cur.Properties = openapi3.Schemas{"n": &openapi3.SchemaRef{Value: next}}
		cur = next
	}
	if out := openAPISchema(root); out == nil {
		t.Fatal("openAPISchema returned nil")
	}
}
