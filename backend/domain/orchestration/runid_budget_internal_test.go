package orchestration

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

// The generated run identifiers land in runtime_invocation.run_id and
// runtime_invocation_attempt.attempt_id. Production case IDs are
// "case-<uuid>" (41 chars), and the dispatcher retry path appends -retryN to the
// checkpoint ID, so the worst case must be asserted rather than assumed.
func TestRunIDsFitInvocationColumns(t *testing.T) {
	caseID := "case-" + strings.Repeat("a", 36) // 41 chars, same shape as case-<uuid>
	codes := []entity.MagiCode{"melchior", "balthasar", "casper"}
	phases := []string{"investigate", "reconsider"}

	for _, code := range codes {
		for _, phase := range phases {
			for attempt := 0; attempt <= 9; attempt++ {
				for round := 1; round <= 3; round++ {
					checkpoint := checkpointRunID(caseID, code, round, phase)
					execution := executionRunID(caseID, code, attempt, round, phase)
					// The dispatcher retry path appends -retryN to the checkpoint ID.
					retry := checkpoint + "-retry9"
					for _, id := range []string{checkpoint, execution, retry} {
						if len(id) > maxInvocationRunIDBytes {
							t.Fatalf("%s: id length %d exceeds %d", id, len(id), maxInvocationRunIDBytes)
						}
					}
				}
			}
		}
	}
}
