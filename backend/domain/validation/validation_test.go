package validation

import (
	"encoding/json"
	"strings"
	"testing"

	einojsonschema "github.com/eino-contrib/jsonschema"

	"github.com/jamespud/magi/backend/domain/entity"
)

type sampleArgs struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestTypedValidator_ValidThenUnmarshal(t *testing.T) {
	v := NewJSONSchemaValidator()
	gen := NewReflectSchemaGenerator()
	tv, err := NewTypedValidator[sampleArgs](gen, v)
	if err != nil {
		t.Fatalf("new typed validator: %v", err)
	}

	got, res := tv.ValidateAndUnmarshal([]byte(`{"name":"mel","age":3}`))
	if res == nil || !res.Valid {
		t.Fatalf("expected valid, got %+v", res)
	}
	if got == nil || got.Name != "mel" || got.Age != 3 {
		t.Fatalf("unexpected unmarshal result: %+v", got)
	}
}

func TestTypedValidator_InvalidTypeMismatch(t *testing.T) {
	v := NewJSONSchemaValidator()
	gen := NewReflectSchemaGenerator()
	tv, err := NewTypedValidator[sampleArgs](gen, v)
	if err != nil {
		t.Fatalf("new typed validator: %v", err)
	}

	got, res := tv.ValidateAndUnmarshal([]byte(`{"name":"mel","age":"old"}`))
	if res != nil && res.Valid {
		t.Fatalf("expected invalid for type mismatch")
	}
	if got != nil {
		t.Fatalf("expected nil T on validation failure")
	}
	if res == nil || len(res.Violations) == 0 {
		t.Fatalf("expected violations, got %+v", res)
	}
}

func TestTypedValidator_MalformedJSON(t *testing.T) {
	v := NewJSONSchemaValidator()
	gen := NewReflectSchemaGenerator()
	tv, err := NewTypedValidator[sampleArgs](gen, v)
	if err != nil {
		t.Fatalf("new typed validator: %v", err)
	}

	got, res := tv.ValidateAndUnmarshal([]byte(`not json`))
	if res != nil && res.Valid {
		t.Fatalf("expected invalid for malformed json")
	}
	if got != nil {
		t.Fatalf("expected nil T on malformed json")
	}
}

func TestSchema_DecisionTask_UsesJSONTagNames(t *testing.T) {
	sch := einojsonschema.Reflect(entity.DecisionTask{})
	b, err := json.Marshal(sch)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	raw := string(b)

	// PascalCase field names that must NOT appear in the schema
	pascalFields := []string{`"Code"`, `"Description"`, `"Topic"`, `"Rationale"`, `"Key"`, `"Value"`, `"Hard"`}
	for _, f := range pascalFields {
		if strings.Contains(raw, f) {
			t.Errorf("schema contains PascalCase field name %s, expected json-tagged lowercase names only", f)
		}
	}

	// Lowercase json-tagged names that MUST appear in the schema
	lowerFields := []string{`"code"`, `"description"`, `"topic"`, `"rationale"`, `"key"`, `"value"`, `"hard"`}
	for _, f := range lowerFields {
		if !strings.Contains(raw, f) {
			t.Errorf("schema missing expected json-tagged field name %s", f)
		}
	}
}

type nestedInner struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type nestedOuter struct {
	CanonicalQuestion string        `json:"canonical_question"`
	Dimensions        []nestedInner `json:"dimensions"`
}

func TestTypedValidator_NestedStructWithJSONTags(t *testing.T) {
	v := NewJSONSchemaValidator()
	gen := NewReflectSchemaGenerator()
	tv, err := NewTypedValidator[nestedOuter](gen, v)
	if err != nil {
		t.Fatalf("new typed validator: %v", err)
	}

	// Valid JSON with lowercase field names (what LLM would produce)
	validJSON := `{
		"canonical_question": "Should we migrate?",
		"dimensions": [
			{"code": "cost", "description": "Migration cost analysis"}
		]
	}`

	got, res := tv.ValidateAndUnmarshal([]byte(validJSON))
	if res == nil || !res.Valid {
		t.Fatalf("expected valid for nested JSON with json tags, got violations: %+v", res)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.CanonicalQuestion != "Should we migrate?" {
		t.Fatalf("expected 'Should we migrate?', got %q", got.CanonicalQuestion)
	}
	if len(got.Dimensions) != 1 || got.Dimensions[0].Code != "cost" {
		t.Fatalf("unexpected dimensions: %+v", got.Dimensions)
	}

	// Invalid JSON with PascalCase field names (what LLM would produce without json tags)
	invalidJSON := `{
		"canonical_question": "Should we migrate?",
		"dimensions": [
			{"Code": "cost", "Description": "Migration cost analysis"}
		]
	}`

	got2, res2 := tv.ValidateAndUnmarshal([]byte(invalidJSON))
	if res2 != nil && res2.Valid {
		t.Fatalf("expected invalid for PascalCase field names with additionalProperties:false")
	}
	if got2 != nil {
		t.Fatalf("expected nil T on validation failure for PascalCase input")
	}
}
