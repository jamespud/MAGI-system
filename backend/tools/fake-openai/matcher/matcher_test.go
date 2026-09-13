package matcher

import "testing"

func TestClassify_MatchesBackendNudges(t *testing.T) {
	const agentSystem = "You are Melchior, the scientist. Decide carefully."
	cases := []struct {
		name     string
		req      Request
		wantKind Kind
		wantRole string
	}{
		{"decision task", Request{System: "commander", User: "Output the DecisionTask JSON now."}, KindDecisionTask, ""},
		{"final report", Request{System: "commander", User: "Output the FinalReportData JSON now."}, KindFinalReport, ""},
		{"final report re-output", Request{System: "commander", User: "Re-output the FinalReportData JSON now, citing at least one of the provided IDs in key_evidence_ids/key_claim_ids."}, KindFinalReport, ""},
		{"vote", Request{System: agentSystem, User: "Evidence gate passed. Now output the Vote JSON."}, KindVote, "melchior"},
		{"vote after reflection", Request{System: agentSystem, User: "Reflection recorded. Now output the Vote JSON."}, KindVote, "melchior"},
		{"reflection", Request{System: agentSystem, User: "Evidence gate passed. Now output the Reflection JSON."}, KindReflection, "melchior"},
		{"question defaults to summary", Request{System: agentSystem, User: "Should we adopt the change?"}, KindEvidenceSummary, "melchior"},
		{"compaction", Request{System: "You are a lossy summarizer for an agent run.", User: "Summarize the following agent working-memory history so the run can continue. ..."}, KindCompaction, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, role := Classify(tc.req)
			if kind != tc.wantKind || role != tc.wantRole {
				t.Fatalf("Classify = (%s, %q), want (%s, %q)", kind, role, tc.wantKind, tc.wantRole)
			}
		})
	}
}

func TestRoleIn_PrefersExplicitAgentRolePhrasing(t *testing.T) {
	// A debate packet may mention other agents; the explicit opening must win.
	system := "You are Melchior, the scientist.\n--- RECONSIDERATION ---\nBalthasar argued for caution; Casper disagreed."
	if got := RoleIn(system); got != "melchior" {
		t.Fatalf("RoleIn = %q, want melchior", got)
	}
	if got := RoleIn("You are the Commander."); got != "" {
		t.Fatalf("RoleIn(commander) = %q, want empty", got)
	}
}
