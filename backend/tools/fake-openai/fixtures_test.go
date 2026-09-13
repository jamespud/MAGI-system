package main

import (
	"fmt"
	"sort"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/validation"
	"github.com/jamespud/magi/backend/tools/fake-openai/scenario"
)

// TestFixtures_SatisfyTheRealSchemas runs every embedded fixture through the same
// TypedValidator the backend uses for LLM output. A schema change therefore
// breaks this test rather than surfacing later as a confusing E2E failure, and a
// fixture can never be "fake-valid" in a way production would reject.
func TestFixtures_SatisfyTheRealSchemas(t *testing.T) {
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()

	task, err := validation.NewTypedValidator[entity.DecisionTask](gen, val)
	if err != nil {
		t.Fatalf("validator(DecisionTask): %v", err)
	}
	report, err := validation.NewTypedValidator[entity.FinalReportData](gen, val)
	if err != nil {
		t.Fatalf("validator(FinalReportData): %v", err)
	}
	summary, err := validation.NewTypedValidator[entity.EvidenceSummary](gen, val)
	if err != nil {
		t.Fatalf("validator(EvidenceSummary): %v", err)
	}
	vote, err := validation.NewTypedValidator[entity.Vote](gen, val)
	if err != nil {
		t.Fatalf("validator(Vote): %v", err)
	}
	reflection, err := validation.NewTypedValidator[entity.Reflection](gen, val)
	if err != nil {
		t.Fatalf("validator(Reflection): %v", err)
	}

	type fixtureCase struct {
		key      string
		validate func([]byte) (bool, string)
	}
	cases := []fixtureCase{
		{"decision_task", func(b []byte) (bool, string) {
			_, res := task.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"final_report", func(b []byte) (bool, string) {
			_, res := report.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"evidence_summary", func(b []byte) (bool, string) {
			_, res := summary.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"vote.melchior", func(b []byte) (bool, string) {
			_, res := vote.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"vote.balthasar", func(b []byte) (bool, string) {
			_, res := vote.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"vote.casper", func(b []byte) (bool, string) {
			_, res := vote.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
		{"reflection", func(b []byte) (bool, string) {
			_, res := reflection.ValidateAndUnmarshal(b)
			return valid(res), violations(res)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			raw, ok := scenario.Fixture(tc.key)
			if !ok {
				t.Fatalf("fixture %q is missing", tc.key)
			}
			if ok, detail := tc.validate(raw); !ok {
				t.Fatalf("fixture %q does not satisfy the real schema: %s", tc.key, detail)
			}
		})
	}

	// Every embedded fixture must be covered above (except the plain-text
	// compaction answer), so adding a fixture cannot silently skip validation.
	keys, err := scenario.FixtureKeys()
	if err != nil {
		t.Fatalf("fixture keys: %v", err)
	}
	sort.Strings(keys)
	covered := map[string]bool{"compaction": true}
	for _, tc := range cases {
		covered[tc.key] = true
	}
	for _, key := range keys {
		if !covered[key] {
			t.Fatalf("fixture %q has no schema validation; add it to this test", key)
		}
	}
}

// TestVoteFixtures_CarryTheirRoleDimensions pins the per-role requirement that
// the backend's ValidateVoteDimensions enforces at runtime: each agent's vote
// must carry exactly that role's utility dimensions.
func TestVoteFixtures_CarryTheirRoleDimensions(t *testing.T) {
	want := map[string][]string{
		"melchior":  {"correctness", "efficiency", "feasibility"},
		"balthasar": {"safety", "reversibility", "maintainability"},
		"casper":    {"opportunity", "innovation", "user_value"},
	}
	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	voteValidator, err := validation.NewTypedValidator[entity.Vote](gen, val)
	if err != nil {
		t.Fatalf("validator(Vote): %v", err)
	}
	for role, dims := range want {
		raw, ok := scenario.Fixture("vote." + role)
		if !ok {
			t.Fatalf("missing vote fixture for %s", role)
		}
		parsed, res := voteValidator.ValidateAndUnmarshal(raw)
		if !valid(res) {
			t.Fatalf("vote.%s invalid: %s", role, violations(res))
		}
		if err := runtimeValidateVoteDimensions(parsed, dims); err != nil {
			t.Fatalf("vote.%s: %v", role, err)
		}
	}
}

// runtimeValidateVoteDimensions mirrors runtime.ValidateVoteDimensions without
// importing the runtime package (which would pull the whole agent loop in).
func runtimeValidateVoteDimensions(vote *entity.Vote, dimensions []string) error {
	want := make(map[string]bool, len(dimensions))
	for _, d := range dimensions {
		want[d] = true
	}
	seen := make(map[string]bool, len(vote.UtilityScores))
	for _, us := range vote.UtilityScores {
		if !want[us.DimensionCode] {
			return fmt.Errorf("utility dimension %q not in the role's objective function", us.DimensionCode)
		}
		if seen[us.DimensionCode] {
			return fmt.Errorf("duplicate utility dimension %q", us.DimensionCode)
		}
		seen[us.DimensionCode] = true
	}
	for d := range want {
		if !seen[d] {
			return fmt.Errorf("missing utility dimension %q", d)
		}
	}
	return nil
}

func valid(res *validation.ValidationResult) bool { return res != nil && res.Valid }

func violations(res *validation.ValidationResult) string {
	if res == nil {
		return "nil validation result"
	}
	var b []byte
	for _, v := range res.Violations {
		b = append(b, (v.Code + ": " + v.Message + "; ")...)
	}
	if len(b) == 0 {
		return "invalid (no violation detail)"
	}
	return string(b)
}
