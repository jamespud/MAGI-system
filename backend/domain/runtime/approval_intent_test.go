package runtime

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/execution"
)

// Issue #9 requires that an invocation whose JSON is only reordered still
// counts as the same intent, so a legitimate retry keeps its decision, while a
// change to any business argument or to the tool produces a different intent. A
// naive fix that compared raw argument strings would pass the "different
// arguments" test but wrongly demand a second approval here, which is why this
// guards the canonical encoding the approval key depends on.
func TestApprovalIntentDigest_KeyOrderDoesNotChangeIntent(t *testing.T) {
	base := execution.ApprovalIntentDigest("rollout", canonicalToolArguments(`{"percent":5,"window":15}`))
	reordered := execution.ApprovalIntentDigest("rollout", canonicalToolArguments(`{"window":15,"percent":5}`))
	changedValue := execution.ApprovalIntentDigest("rollout", canonicalToolArguments(`{"percent":10,"window":15}`))
	changedTool := execution.ApprovalIntentDigest("observe", canonicalToolArguments(`{"percent":5,"window":15}`))

	if base != reordered {
		t.Fatal("reordered JSON keys must describe the same invocation intent")
	}
	if base == changedValue {
		t.Fatal("a changed argument value must produce a different invocation intent")
	}
	if base == changedTool {
		t.Fatal("a different tool must produce a different invocation intent")
	}
}
