package dto

import (
	"encoding/json"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

// The three dissent states must be distinguishable in every JSON projection.
// "Not recorded" (legacy) must never serialize the same way as a confirmed
// "no dissent" (issue #13).
func TestDissentTriState_HTTPAndExportProjections(t *testing.T) {
	c := &entity.DecisionCase{ID: "c1", Question: "q"}

	present := []entity.Dissent{{
		AgentCode: entity.MagiCodeCasper, Decision: entity.VoteDecisionReject,
		Reasoning: "risk", Conditions: []entity.DecisionCondition{{Statement: "rollback", MustHold: true}},
	}}
	cases := []struct {
		name      string
		dissent   []entity.Dissent
		wantState entity.DissentState
		wantJSON  string // the marshaled `dissent` member
	}{
		{"legacy unknown", nil, entity.DissentUnknown, "null"},
		{"recorded none", []entity.Dissent{}, entity.DissentNone, "[]"},
		{"recorded present", present, entity.DissentPresent, "["},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &entity.Resolution{ID: "res", CaseID: "c1", FinalDecision: entity.VoteDecisionApprove,
				FinalReport: "# report", Dissent: tc.dissent}

			httpResp := FromCaseWithDissent(c, res, res.Dissent)
			if httpResp.DissentState != string(tc.wantState) {
				t.Fatalf("HTTP dissent_state = %q, want %q", httpResp.DissentState, tc.wantState)
			}
			assertDissentJSON(t, "HTTP", httpResp, tc.wantJSON)

			export := FromResolutionExport(res)
			if export.DissentState != string(tc.wantState) {
				t.Fatalf("export dissent_state = %q, want %q", export.DissentState, tc.wantState)
			}
			assertDissentJSON(t, "export", export, tc.wantJSON)
		})
	}
}

// assertDissentJSON checks the raw JSON `dissent` member so null, [] and a
// populated array stay distinct on the wire.
func assertDissentJSON(t *testing.T, label string, v any, prefix string) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Dissent json.RawMessage `json:"dissent"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	got := string(probe.Dissent)
	if prefix == "[" {
		if len(got) == 0 || got[0] != '[' || got == "[]" {
			t.Fatalf("%s dissent JSON = %s, want a populated array", label, got)
		}
		return
	}
	if got != prefix {
		t.Fatalf("%s dissent JSON = %s, want %s", label, got, prefix)
	}
}
