package orchestration

import (
	"context"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type Dispatcher struct {
	agentLoop runtime.MagiRuntime
}

func NewDispatcher(agentLoop runtime.MagiRuntime) *Dispatcher {
	return &Dispatcher{agentLoop: agentLoop}
}

func (d *Dispatcher) Dispatch(
	ctx context.Context,
	case_ *entity.DecisionCase,
	task *entity.DecisionTask,
	configs []*entity.MagiConfig,
) []*runtime.LoopResult {
	results := make([]*runtime.LoopResult, len(configs))
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, c *entity.MagiConfig) {
			defer wg.Done()
			// Per-Magi AgentContext: each Magi gets its own context with its
			// specific config (persona/objective/tools) via cfg in agent_loop.Run.
			actx := &runtime.AgentContext{CaseID: case_.ID, Task: derefTask(task)}
			r, _ := d.agentLoop.Run(ctx, c, actx)
			if r == nil {
				r = &runtime.LoopResult{Status: runtime.LoopStatusError}
			}
			results[idx] = r
		}(i, cfg)
	}
	wg.Wait()
	return results
}

func (d *Dispatcher) DispatchReconsider(
	ctx context.Context,
	case_ *entity.DecisionCase,
	task *entity.DecisionTask,
	packet entity.DebatePacket,
	prevResults []*runtime.LoopResult,
	configs []*entity.MagiConfig,
) []*runtime.LoopResult {
	results := make([]*runtime.LoopResult, len(configs))
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, c *entity.MagiConfig) {
			defer wg.Done()
			var prevVote *entity.Vote
			if idx < len(prevResults) && prevResults[idx] != nil {
				prevVote = prevResults[idx].Vote
			}
			actx := &runtime.AgentContext{
				CaseID: case_.ID,
				Task:   derefTask(task),
				DebateContext: &runtime.DebateContext{Packet: packet, PreviousVote: prevVote},
			}
			r, _ := d.agentLoop.Run(ctx, c, actx)
			if r == nil {
				r = &runtime.LoopResult{Status: runtime.LoopStatusError}
			}
			results[idx] = r
		}(i, cfg)
	}
	wg.Wait()
	return results
}

func derefTask(t *entity.DecisionTask) entity.DecisionTask {
	if t == nil {
		return entity.DecisionTask{}
	}
	return *t
}
