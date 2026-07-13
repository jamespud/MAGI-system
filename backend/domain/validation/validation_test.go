package validation

import "testing"

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
