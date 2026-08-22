package magi

import (
	"encoding/json"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// TestFeedbackArgsSchema_IsValidJSON guards the check_output tool's declared
// argument schema. A malformed schema makes every check_output call fail at
// argument validation with SCHEMA_INVALID_JSON before the executor even runs.
func TestFeedbackArgsSchema_IsValidJSON(t *testing.T) {
	if !json.Valid([]byte(feedbackArgsSchema)) {
		t.Fatalf("feedbackArgsSchema is not valid JSON: %s", feedbackArgsSchema)
	}

	var doc any
	if err := sonic.Unmarshal([]byte(feedbackArgsSchema), &doc); err != nil {
		t.Fatalf("sonic.Unmarshal(feedbackArgsSchema): %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("magi://check_output_args.json", doc); err != nil {
		t.Fatalf("jsonschema compile(check_output args): %v", err)
	}
}
