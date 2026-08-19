package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// agentLoopHolder must satisfy the same MagiRuntime contract that the agent
// loop implements, so the delegate tool can hold it until the loop is built.
var _ runtime.MagiRuntime = (*agentLoopHolder)(nil)

func TestAgentLoopHolder_UninitializedRunFails(t *testing.T) {
	h := provideAgentLoopHolder()
	_, err := h.Run(context.Background(), &entity.MagiConfig{}, &runtime.AgentContext{})
	if err == nil {
		t.Fatal("expected error when running an uninitialized holder")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}
