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
	agentLoop        runtime.MagiRuntime
	contextBuilder   *memory.ContextBuilder
	bindingsProvider ToolBindingsProvider
}

// ToolBindingsProvider resolves the enabled tool bindings for a user at run
// time (e.g. user-scoped plugin bindings).
type ToolBindingsProvider interface {
	BindingsForUser(ctx context.Context, userID int64) ([]entity.ToolBinding, error)
}

// DispatcherOption configures a Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithToolBindingsProvider wires a per-user tool-binding resolver.
func WithToolBindingsProvider(p ToolBindingsProvider) DispatcherOption {
	return func(d *Dispatcher) { d.bindingsProvider = p }
}

func NewDispatcher(agentLoop runtime.MagiRuntime, contextBuilder *memory.ContextBuilder, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{agentLoop: agentLoop, contextBuilder: contextBuilder}
	for _, o := range opts {
		o(d)
	}
	return d
}

func (d *Dispatcher) buildBase(ctx context.Context, case_ *entity.DecisionCase, task *entity.DecisionTask) *runtime.AgentContext {
	if d.contextBuilder != nil {
		if actx, err := d.contextBuilder.Build(ctx, case_, task, nil, nil); err == nil && actx != nil {
			if case_ != nil && case_.UserID != 0 {
				actx.UserID = fmt.Sprintf("%d", case_.UserID)
			}
			d.applyBindings(ctx, case_, actx)
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
	userID := ""
	if case_ != nil && case_.UserID != 0 {
		userID = fmt.Sprintf("%d", case_.UserID)
	}
	actx := &runtime.AgentContext{CaseID: caseID, UserID: userID, Task: t, Constraints: constraints}
	d.applyBindings(ctx, case_, actx)
	return actx
}

func (d *Dispatcher) applyBindings(ctx context.Context, case_ *entity.DecisionCase, actx *runtime.AgentContext) {
	if d.bindingsProvider == nil || case_ == nil || case_.UserID == 0 {
		return
	}
	if bindings, err := d.bindingsProvider.BindingsForUser(ctx, case_.UserID); err == nil {
		actx.ToolBindings = bindings
	}
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
			actx.RunID = checkpointRunID(case_.ID, entity.MagiCode(c.Code), round, "investigate")
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
			actx.RunID = checkpointRunID(case_.ID, entity.MagiCode(c.Code), round, "reconsider")
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

// checkpointRunID is the stable identity of an agent's working memory for a
// (case, agent, round, phase). Unlike executionRunID it deliberately excludes
// the execution attempt so durable retries resume the same checkpoint.
func checkpointRunID(caseID string, code entity.MagiCode, round int, phase string) string {
	return fmt.Sprintf("%s-%s-r%d-%s", caseID, code, round, phase)
}

func executionRunID(caseID string, code entity.MagiCode, attempt, round int, phase string) string {
	attemptPart := ""
	if attempt > 0 {
		attemptPart = fmt.Sprintf("-a%d", attempt)
	}
	return fmt.Sprintf("%s-%s%s-r%d-%s", caseID, code, attemptPart, round, phase)
}

func derefTask(t *entity.DecisionTask) entity.DecisionTask {
	if t == nil {
		return entity.DecisionTask{}
	}
	return *t
}
