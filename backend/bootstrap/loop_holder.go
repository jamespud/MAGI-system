package bootstrap

import (
	"context"
	"fmt"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

// agentLoopHolder breaks the Fx dependency cycle between the agent loop and
// the tool executor: the loop needs the executor to run tools, while the
// executor's delegate tool needs the loop to spawn sub-investigations.
//
// The holder is provided with no dependencies and is populated once the agent
// loop is constructed. Components that would otherwise close the cycle (the
// tool executor) capture the holder instead of the raw loop and resolve it
// lazily at execution time, which is always after the app has finished
// starting.
type agentLoopHolder struct {
	mu   sync.Mutex
	loop *runtime.AgentLoop
}

// provideAgentLoopHolder is an Fx provider for the empty holder. It has no
// dependencies, so it cannot participate in the cycle.
func provideAgentLoopHolder() *agentLoopHolder {
	return &agentLoopHolder{}
}

// set stores the constructed agent loop. Called by provideAgentLoop once the
// loop has been fully built.
func (h *agentLoopHolder) set(loop *runtime.AgentLoop) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loop = loop
}

// Run implements runtime.MagiRuntime by forwarding to the underlying agent
// loop, resolving it lazily so the holder can be wired before the loop exists.
func (h *agentLoopHolder) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	h.mu.Lock()
	loop := h.loop
	h.mu.Unlock()
	if loop == nil {
		return nil, fmt.Errorf("agent loop not initialized yet")
	}
	return loop.Run(ctx, cfg, actx)
}
