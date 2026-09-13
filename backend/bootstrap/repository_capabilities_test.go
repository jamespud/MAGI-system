package bootstrap

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/port"
)

// The orchestrator can fence a status write together with its event only when
// the wired repository implements the optional commit capabilities. Those
// interfaces are satisfied structurally, so a decorator that forgets to
// forward them still compiles and then silently degrades every case to the
// non-atomic path (visible as magi_commit_fence_fallback_total). Pin the
// wiring instead of relying on that counter as the first symptom.
func TestProvidedRepositoryKeepsAtomicCommitCapabilities(t *testing.T) {
	repo := provideRepository(nil)
	if _, ok := repo.(port.TerminalCommitter); !ok {
		t.Fatal("wired repository does not implement port.TerminalCommitter")
	}
	if _, ok := repo.(port.StatusTransitionCommitter); !ok {
		t.Fatal("wired repository does not implement port.StatusTransitionCommitter")
	}
	if _, ok := repo.CaseRepo().(port.ConditionalCaseStatusWriter); !ok {
		t.Fatal("wired case repository does not implement port.ConditionalCaseStatusWriter")
	}
}
