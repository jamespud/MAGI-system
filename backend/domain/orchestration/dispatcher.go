package orchestration

import (
	"context"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/runtime"
	"golang.org/x/sync/errgroup"
)

type Dispatcher struct {
	agentLoop      runtime.MagiRuntime
	contextBuilder *memory.ContextBuilder
}

func NewDispatcher(agentLoop runtime.MagiRuntime, contextBuilder *memory.ContextBuilder) *Dispatcher {
	return &Dispatcher{agentLoop: agentLoop, contextBuilder: contextBuilder}
}

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
	g, gctx := errgroup.WithContext(ctx)
	for i, cfg := range configs {
		i, c := i, cfg
		g.Go(func() error {
			actx := *base
			actx.RunID = fmt.Sprintf("%s-%s-r%d-investigate", case_.ID, c.Code, round)
			r, _ := d.agentLoop.Run(gctx, c, &actx)
			if r == nil {
				r = &runtime.LoopResult{Status: runtime.LoopStatusError}
			}
			results[i] = r
			return nil
		})
	}
	_ = g.Wait()
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
	g, gctx := errgroup.WithContext(ctx)
	for i, cfg := range configs {
		i, c := i, cfg
		g.Go(func() error {
			var prevVote *entity.Vote
			if i < len(prevResults) && prevResults[i] != nil {
				prevVote = prevResults[i].Vote
			}
			actx := *base
			actx.RunID = fmt.Sprintf("%s-%s-r%d-reconsider", case_.ID, c.Code, round)
			actx.DebateContext = &runtime.DebateContext{Packet: packet, PreviousVote: prevVote}
			r, _ := d.agentLoop.Run(gctx, c, &actx)
			if r == nil {
				r = &runtime.LoopResult{Status: runtime.LoopStatusError}
			}
			results[i] = r
			return nil
		})
	}
	_ = g.Wait()
	return results
}

func derefTask(t *entity.DecisionTask) entity.DecisionTask {
	if t == nil {
		return entity.DecisionTask{}
	}
	return *t
}
