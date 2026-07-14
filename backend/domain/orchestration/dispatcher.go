package orchestration

import (
	"context"
	"fmt"
	"sync"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type Dispatcher struct {
	agentLoop      runtime.MagiRuntime
	contextBuilder *memory.ContextBuilder
}

func NewDispatcher(agentLoop runtime.MagiRuntime, contextBuilder *memory.ContextBuilder) *Dispatcher {
	return &Dispatcher{agentLoop: agentLoop, contextBuilder: contextBuilder}
}

// buildBase returns the shared AgentContext for one dispatch, optionally
// retrieving RAG knowledge via the ContextBuilder. When contextBuilder is
// nil (standalone CLI without knowledge), it falls back to a minimal context.
func (d *Dispatcher) buildBase(ctx context.Context, case_ *entity.DecisionCase, task *entity.DecisionTask) *runtime.AgentContext {
	if d.contextBuilder != nil {
		if actx, err := d.contextBuilder.Build(ctx, case_, task, nil, nil); err == nil && actx != nil {
			return actx
		}
	}
	var t entity.DecisionTask
	if task != nil {
		t = *task
	}
	var constraints []entity.Constraint
	if case_ != nil {
		constraints = case_.Constraints
	}
	caseID := ""
	if case_ != nil {
		caseID = case_.ID
	}
	return &runtime.AgentContext{CaseID: caseID, Task: t, Constraints: constraints}
}

func (d *Dispatcher) Dispatch(
	ctx context.Context,
	case_ *entity.DecisionCase,
	task *entity.DecisionTask,
	configs []*entity.MagiConfig,
	round int,
) []*runtime.LoopResult {
	results := make([]*runtime.LoopResult, len(configs))
	base := d.buildBase(ctx, case_, task)
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, c *entity.MagiConfig) {
			defer wg.Done()
			actx := *base
			actx.RunID = fmt.Sprintf("%s-%s-r%d-investigate", case_.ID, c.Code, round)
			r, _ := d.agentLoop.Run(ctx, c, &actx)
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
	round int,
) []*runtime.LoopResult {
	results := make([]*runtime.LoopResult, len(configs))
	base := d.buildBase(ctx, case_, task)
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(idx int, c *entity.MagiConfig) {
			defer wg.Done()
			var prevVote *entity.Vote
			if idx < len(prevResults) && prevResults[idx] != nil {
				prevVote = prevResults[idx].Vote
			}
			actx := *base
			actx.RunID = fmt.Sprintf("%s-%s-r%d-reconsider", case_.ID, c.Code, round)
			actx.DebateContext = &runtime.DebateContext{Packet: packet, PreviousVote: prevVote}
			r, _ := d.agentLoop.Run(ctx, c, &actx)
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
