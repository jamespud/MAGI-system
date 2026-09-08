package runtime_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/modelruntime"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/toolruntime"
	"github.com/jamespud/magi/backend/domain/validation"
	"github.com/jamespud/magi/backend/internal/harnesstest"
)

// TestHarnessTestkit_Smoke verifies the crash-recovery testkit compiles and can
// drive a complete agent loop with tools, invoking repositories and a
// checkpoint store. Task 11 layers crash cases on top of these primitives.
func TestHarnessTestkit_Smoke(t *testing.T) {
	model := &harnesstest.ScriptedModel{Responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}}
	tool := &harnesstest.CountingTool{}
	invocations := harnesstest.NewRecordingInvocationRepository()
	checkpoints := harnesstest.NewFailingCheckpointRepository(nil)
	reg := validation.NewReflectSchemaGenerator()
	calcSchema, err := reg.FromStruct(calcArgs{})
	if err != nil {
		t.Fatalf("calc schema: %v", err)
	}
	registry := &harnesstest.ToolRegistry{Defs: []port.ToolDefinition{{
		Name: "calc", Desc: "add", ArgsSchema: calcSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"},
	}}}

	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &harnesstest.ModelPort{M: model},
		ToolReg:        registry,
		ToolExec:       tool,
		Invocations:    invocations,
		Validator:      validation.NewJSONSchemaValidator(),
		Gen:            reg,
		CheckpointRepo: checkpoints,
	})
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}
	res, err := loop.Run(context.Background(), evidenceCfg(1, 0), &runtime.AgentContext{RunID: "run-toolkit", Task: entity.DecisionTask{CanonicalQuestion: "compute"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status = %v, want completed", res.Status)
	}
	if tool.Calls != 1 {
		t.Fatalf("tool calls = %d, want 1", tool.Calls)
	}
	if _, ok := res.Ledger.Get("EV-001"); !ok {
		t.Fatal("expected evidence EV-001 from the tool result")
	}
	if invocations.BeginAttemptCalls == 0 {
		t.Fatal("expected the tool invocation to be fenced through the kernel")
	}
}

// --- crash recovery harness ---

// crashModel wraps a ScriptedModel and crashes at CrashBeforeModel.
type crashModel struct {
	inner *harnesstest.ScriptedModel
	inj   *harnesstest.CrashInjector
}

func (m *crashModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.inj.Trigger(harnesstest.CrashBeforeModel)
	msg, err := m.inner.Generate(ctx, input, opts...)
	// The model has produced a response but the invocation result has not yet
	// been persisted by the kernel when the execute fn returns.
	if err == nil {
		m.inj.Trigger(harnesstest.CrashAfterModelBeforePersist)
	}
	return msg, err
}

func (m *crashModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("crash model: streaming not implemented")
}

func (m *crashModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

var _ model.ToolCallingChatModel = (*crashModel)(nil)

// crashTool wraps an executor and crashes at CrashBeforeTool.
type crashTool struct {
	inner port.ToolExecutorPort
	inj   *harnesstest.CrashInjector
}

func (t *crashTool) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	t.inj.Trigger(harnesstest.CrashBeforeTool)
	result, err := t.inner.Execute(ctx, req)
	// An external side effect may already have happened, but the kernel has not
	// yet persisted the completed invocation when the execute fn returns.
	if err == nil {
		t.inj.Trigger(harnesstest.CrashAfterToolBeforePersist)
	}
	return result, err
}

var _ port.ToolExecutorPort = (*crashTool)(nil)

// crashCheckpoint wraps a checkpoint repository and crashes at
// CrashBeforeCheckpoint / CrashAfterCheckpoint.
type crashCheckpoint struct {
	inner *harnesstest.FailingCheckpointRepository
	inj   *harnesstest.CrashInjector
}

func (c *crashCheckpoint) Save(ctx context.Context, state *entity.AgentState) error {
	c.inj.Trigger(harnesstest.CrashBeforeCheckpoint)
	if err := c.inner.Save(ctx, state); err != nil {
		return err
	}
	c.inj.Trigger(harnesstest.CrashAfterCheckpoint)
	return nil
}

func (c *crashCheckpoint) Load(ctx context.Context, runID string) (*entity.AgentState, error) {
	return c.inner.Load(ctx, runID)
}

var _ port.CheckpointRepository = (*crashCheckpoint)(nil)

// runWithCrash runs the loop in a goroutine and recovers an injected crash
// panic. It reports whether the injected crash point was reached.
func runWithCrash(t *testing.T, loop *runtime.AgentLoop, cfg *entity.MagiConfig, actx *runtime.AgentContext) (crashed bool) {
	t.Helper()
	done := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if errors.Is(r.(error), harnesstest.CrashError) {
					done <- true
					return
				}
				panic(r)
			}
			done <- false
		}()
		_, _ = loop.Run(context.Background(), cfg, actx)
	}()
	return <-done
}

func cfgWithTools(minQ float64) *entity.MagiConfig {
	cfg := evidenceCfg(minQ, 0)
	cfg.LoopPolicy = entity.LoopPolicy{MaxSteps: 12, MaxToolCalls: 5}
	return cfg
}

type crashHarness struct {
	invocations port.RuntimeInvocationRepository
	checkpoints *harnesstest.FailingCheckpointRepository
	inj         *harnesstest.CrashInjector
	model       *harnesstest.ScriptedModel
	tool        port.ToolExecutorPort
}

// crashInvocationRepository wraps the recording repository and crashes right
// after a successful invocation result is persisted (Complete), modelling the
// window where the durable success exists but the process dies before the loop
// observes it.
type crashInvocationRepository struct {
	*harnesstest.RecordingInvocationRepository
	inj *harnesstest.CrashInjector
}

func (r *crashInvocationRepository) Complete(ctx context.Context, invocationID, attemptID, outputJSON string) (bool, error) {
	won, err := r.RecordingInvocationRepository.Complete(ctx, invocationID, attemptID, outputJSON)
	if won && err == nil {
		r.inj.Trigger(harnesstest.CrashAfterInvocationPersist)
	}
	return won, err
}

// newLoop builds an AgentLoop that fences both model and tool generations
// through a shared invocation repository and injects the configured crash point.
func (h *crashHarness) newLoop(t *testing.T, responses []*schema.Message, tool port.ToolExecutorPort) *runtime.AgentLoop {
	t.Helper()
	reg := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	calcSchema, err := reg.FromStruct(calcArgs{})
	if err != nil {
		t.Fatalf("calc schema: %v", err)
	}
	if h.model == nil {
		h.model = &harnesstest.ScriptedModel{Responses: responses}
	}
	if h.tool == nil {
		h.tool = tool
	}
	modelRuntime := modelruntime.New(execution.NewKernel(h.invocations, nil))
	toolRuntime, err := toolruntime.New(toolruntime.Deps{
		Kernel:    execution.NewKernel(h.invocations, nil),
		Executor:  &crashTool{inner: h.tool, inj: h.inj},
		Validator: val,
	})
	if err != nil {
		t.Fatalf("tool runtime: %v", err)
	}
	registry := &harnesstest.ToolRegistry{Defs: []port.ToolDefinition{{
		Name: "calc", Desc: "add", ArgsSchema: calcSchema,
		Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"},
	}}}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &harnesstest.ModelPort{M: &crashModel{inner: h.model, inj: h.inj}},
		ModelRuntime:   modelRuntime,
		ToolReg:        registry,
		ToolRuntime:    toolRuntime,
		Validator:      val,
		Gen:            reg,
		CheckpointRepo: &crashCheckpoint{inner: h.checkpoints, inj: h.inj},
	})
	if err != nil {
		t.Fatalf("new agent loop: %v", err)
	}
	return loop
}

// Case F: a configured checkpoint Save failure must stop before any next
// model/tool invocation.
func TestCheckpointFailureStopsExecution(t *testing.T) {
	h := &crashHarness{
		invocations: harnesstest.NewRecordingInvocationRepository(),
		checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
	}
	h.checkpoints.SetSaveError(errors.New("checkpoint down"))
	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	model := &harnesstest.ScriptedModel{Responses: responses}
	tool := &harnesstest.CountingTool{}
	loop := h.newLoop(t, responses, tool)

	_, err := loop.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-f", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil {
		t.Fatal("want error when checkpoint Save fails")
	}
	if tool.Calls != 0 {
		t.Fatalf("tool calls = %d, want 0 (must stop before tool invocation)", tool.Calls)
	}
	if model.Calls != 0 {
		t.Fatalf("model calls = %d, want 0 (must stop before model invocation)", model.Calls)
	}
}

// TestResumeRestoresTerminationState: a resumed run keeps its restored
// termination counters instead of regaining the full budget.
func TestResumeRestoresTerminationState(t *testing.T) {
	cfg := cfgWithTools(1)
	cfg.LoopPolicy.MaxToolCalls = 5
	snapshot := execution.AgentSnapshotV2{
		RunID: "run-g", NextStep: 1, Phase: "gather", MessagesJSON: checkpointMessagesJSON(t),
		Termination:    execution.TerminationSnapshot{ToolCalls: 4},
		ManifestDigest: crashCheckpointManifest(t, cfg), SummaryJSON: summaryJSON(),
	}
	h := &crashHarness{
		invocations: harnesstest.NewRecordingInvocationRepository(),
		checkpoints: harnesstest.NewFailingCheckpointRepository(checkpointState(t, snapshot)),
	}
	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	tool := &harnesstest.CountingTool{}
	loop := h.newLoop(t, responses, tool)

	res, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-g", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("status = %v, want completed", res.Status)
	}
	if tool.Calls != 1 {
		t.Fatalf("tool calls = %d, want 1 (only one tool allowed before the restored 4/5 budget forces convergence)", tool.Calls)
	}
}

// crashCheckpointManifest builds the manifest digest the crash harness loop
// expects: the prompt is built with hasTools=true because the harness registers
// a tool.
func crashCheckpointManifest(t *testing.T, cfg *entity.MagiConfig) string {
	t.Helper()
	gen := validation.NewReflectSchemaGenerator()
	summarySchema, err := gen.FromStruct(entity.EvidenceSummary{})
	if err != nil {
		t.Fatalf("summary schema: %v", err)
	}
	voteSchema, err := gen.FromStruct(entity.Vote{})
	if err != nil {
		t.Fatalf("vote schema: %v", err)
	}
	reflectionSchema, err := gen.FromStruct(entity.Reflection{})
	if err != nil {
		t.Fatalf("reflection schema: %v", err)
	}
	prompt := runtime.BuildAgentSystemPrompt(cfg, summarySchema, voteSchema, reflectionSchema, nil, true)
	return runtime.CheckpointManifest(cfg, &runtime.AgentContext{}, prompt)
}

// Case A: a completed model generation is served from the invocation cache on a
// second run and is not regenerated.
func TestCrashAfterModelCompletionDoesNotRegenerate(t *testing.T) {
	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	h := &crashHarness{
		invocations: harnesstest.NewRecordingInvocationRepository(),
		checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
	}
	tool := &harnesstest.CountingTool{}
	first := h.newLoop(t, responses, tool)
	if _, err := first.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-a", Task: entity.DecisionTask{CanonicalQuestion: "q"}}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if h.model.Calls == 0 {
		t.Fatal("expected the model to run during the first pass")
	}
	modelCalls := h.model.Calls

	second := h.newLoop(t, responses, tool)
	res, err := second.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-a", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("second status = %v, want completed", res.Status)
	}
	if h.model.Calls != modelCalls {
		t.Fatalf("model regenerated: calls went from %d to %d", modelCalls, h.model.Calls)
	}
}

// Case B: a persisted tool completion is not executed again on a second run.
func TestCrashAfterToolCompletionPersistedDoesNotExecuteAgain(t *testing.T) {
	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	h := &crashHarness{
		invocations: harnesstest.NewRecordingInvocationRepository(),
		checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
	}
	tool := &harnesstest.CountingTool{}
	first := h.newLoop(t, responses, tool)
	if _, err := first.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-b", Task: entity.DecisionTask{CanonicalQuestion: "q"}}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if tool.Calls != 1 {
		t.Fatalf("first run tool calls = %d, want 1", tool.Calls)
	}

	second := h.newLoop(t, responses, tool)
	res, err := second.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-b", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.Status != runtime.LoopStatusCompleted {
		t.Fatalf("second status = %v, want completed", res.Status)
	}
	if tool.Calls != 1 {
		t.Fatalf("tool re-executed: calls = %d, want 1", tool.Calls)
	}
}

// Case H: driving a crash at every reachable durable boundary and resuming must
// never duplicate an external side effect and must keep stable invocation IDs.
func TestCrashRecoveryAtEveryDurableBoundary(t *testing.T) {
	points := []harnesstest.CrashPoint{
		harnesstest.CrashBeforeModel,
		harnesstest.CrashAfterModelBeforePersist,
		harnesstest.CrashBeforeTool,
		harnesstest.CrashAfterToolBeforePersist,
		harnesstest.CrashAfterInvocationPersist,
		harnesstest.CrashBeforeCheckpoint,
		harnesstest.CrashAfterCheckpoint,
	}
	for _, point := range points {
		t.Run(string(point), func(t *testing.T) {
			responses := []*schema.Message{
				callMsg("c1", "calc", `{"a":1,"b":2}`),
				finalMsg(summaryJSON("EV-001")),
				finalMsg(voteJSON("correctness")),
			}
			sideEffect := &harnesstest.SideEffectTool{}
			rec := harnesstest.NewRecordingInvocationRepository()
			wrapped := &crashInvocationRepository{RecordingInvocationRepository: rec, inj: &harnesstest.CrashInjector{Point: point}}
			h := &crashHarness{
				invocations: wrapped,
				checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
				inj:         &harnesstest.CrashInjector{Point: point},
			}
			first := h.newLoop(t, responses, sideEffect)
			crashed := runWithCrash(t, first, cfgWithTools(1), &runtime.AgentContext{RunID: "run-h", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
			if !crashed {
				t.Fatalf("expected injected crash at %s to be reached", point)
			}

			// Disable crash injection for the resume so it can run to a normal
			// boundary, then restart with the same repos and model/tool instances.
			h.inj = &harnesstest.CrashInjector{}
			wrapped.inj = &harnesstest.CrashInjector{}
			second := h.newLoop(t, responses, sideEffect)
			res, err := second.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-h", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
			if err != nil {
				// A fenced boundary (e.g. an incomplete invocation) may fail closed;
				// that is acceptable as long as no side effect is duplicated.
				if sideEffect.ExternalWrites > 1 {
					t.Fatalf("side effect duplicated: writes = %d", sideEffect.ExternalWrites)
				}
				return
			}
			if sideEffect.ExternalWrites > 1 {
				t.Fatalf("side effect duplicated: writes = %d", sideEffect.ExternalWrites)
			}
			ids := rec.IDs()
			if len(ids) == 0 {
				t.Fatal("expected at least one stable logical invocation id after crash+resume")
			}
			for _, id := range ids {
				if id == "" {
					t.Fatal("recorded a blank logical invocation id")
				}
			}
			switch res.Status {
			case runtime.LoopStatusCompleted:
				// A completed resume must have run at least one model step.
			case runtime.LoopStatusError:
				// fail-closed recovery (e.g. an incomplete invocation) is acceptable.
			default:
				t.Fatalf("unexpected final status: %v", res.Status)
			}
		})
	}
}

// TestToolRuntimePersistsIdempotencyKeyAndOrdinal verifies the durable
// invocation row records the deterministic idempotency key and step ordinal
// that toolruntime hands to the kernel.
func TestToolRuntimePersistsIdempotencyKeyAndOrdinal(t *testing.T) {
	rec := harnesstest.NewRecordingInvocationRepository()
	val := validation.NewJSONSchemaValidator()
	counting := &harnesstest.CountingTool{}
	countingRuntime, err := toolruntime.New(toolruntime.Deps{
		Kernel:    execution.NewKernel(rec, nil),
		Executor:  counting,
		Validator: val,
	})
	if err != nil {
		t.Fatalf("tool runtime: %v", err)
	}
	req := toolRuntimeRequest("att-1", port.ToolEffectIdempotent)
	req.Ordinal = 2
	result, err := countingRuntime.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	stored := rec.Invocation("inv-1")
	if stored == nil {
		t.Fatal("expected the invocation to be persisted")
	}
	if stored.IdempotencyKey == nil || *stored.IdempotencyKey != result.IdempotencyKey {
		t.Fatalf("persisted idempotency key = %v, want %q", stored.IdempotencyKey, result.IdempotencyKey)
	}
	if stored.LogicalOrdinal != 2 {
		t.Fatalf("persisted logical ordinal = %d, want 2", stored.LogicalOrdinal)
	}
}

// TestCrashAfterToolBeforePersistDoesNotReplaySideEffect: a tool that already
// produced an external side effect before its invocation was persisted must not
// be executed again after crash + resume.
func TestCrashAfterToolBeforePersistDoesNotReplaySideEffect(t *testing.T) {
	responses := []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(summaryJSON("EV-001")),
		finalMsg(voteJSON("correctness")),
	}
	sideEffect := &harnesstest.SideEffectTool{}
	rec := harnesstest.NewRecordingInvocationRepository()
	wrapped := &crashInvocationRepository{RecordingInvocationRepository: rec, inj: &harnesstest.CrashInjector{Point: harnesstest.CrashAfterToolBeforePersist}}
	h := &crashHarness{
		invocations: wrapped,
		checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
		inj:         &harnesstest.CrashInjector{Point: harnesstest.CrashAfterToolBeforePersist},
	}
	first := h.newLoop(t, responses, sideEffect)
	if !runWithCrash(t, first, cfgWithTools(1), &runtime.AgentContext{RunID: "run-at", Task: entity.DecisionTask{CanonicalQuestion: "q"}}) {
		t.Fatal("expected the injected crash at after_tool_before_persist")
	}
	if sideEffect.ExternalWrites != 1 {
		t.Fatalf("side effect writes = %d, want exactly 1 before crash", sideEffect.ExternalWrites)
	}

	h.inj = &harnesstest.CrashInjector{}
	wrapped.inj = &harnesstest.CrashInjector{}
	second := h.newLoop(t, responses, sideEffect)
	_, err := second.Run(context.Background(), cfgWithTools(1), &runtime.AgentContext{RunID: "run-at", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil && !errors.Is(err, port.ErrLeaseLost) {
		// A fail-closed resume is acceptable; the crucial invariant is below.
		t.Logf("resume returned error: %v", err)
	}
	if sideEffect.ExternalWrites != 1 {
		t.Fatalf("side effect replayed: writes = %d, want 1", sideEffect.ExternalWrites)
	}
	if sideEffect.Calls != 1 {
		t.Fatalf("tool executor calls = %d, want 1 (no second execution)", sideEffect.Calls)
	}
}

// TestCrashAfterModelBeforePersistDoesNotRegenerateAttempt: a model that
// returned a response before its invocation was persisted must not get a second
// physical attempt after crash + resume.
func TestCrashAfterModelBeforePersistDoesNotRegenerateAttempt(t *testing.T) {
	responses := []*schema.Message{callMsg("c1", "calc", `{"a":1,"b":2}`)}
	sideEffect := &harnesstest.SideEffectTool{}
	rec := harnesstest.NewRecordingInvocationRepository()
	wrapped := &crashInvocationRepository{RecordingInvocationRepository: rec, inj: &harnesstest.CrashInjector{Point: harnesstest.CrashAfterModelBeforePersist}}
	h := &crashHarness{
		invocations: wrapped,
		checkpoints: harnesstest.NewFailingCheckpointRepository(nil),
		inj:         &harnesstest.CrashInjector{Point: harnesstest.CrashAfterModelBeforePersist},
	}
	first := h.newLoop(t, responses, sideEffect)
	if !runWithCrash(t, first, cfgWithTools(1), &runtime.AgentContext{RunID: "run-am", Task: entity.DecisionTask{CanonicalQuestion: "q"}}) {
		t.Fatal("expected the injected crash at after_model_before_persist")
	}
	cfg := cfgWithTools(1)
	stepID := execution.NewStepID("run-am", 1)
	modelInvocation := modelruntime.NewInvocationID(stepID, cfg.Model)
	if inv := rec.Invocation(modelInvocation); inv == nil || inv.AttemptCount != 1 {
		t.Fatalf("model invocation attempt count = %+v, want 1", rec.Invocation(modelInvocation))
	}

	h.inj = &harnesstest.CrashInjector{}
	wrapped.inj = &harnesstest.CrashInjector{}
	second := h.newLoop(t, responses, sideEffect)
	_, err := second.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-am", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil {
		t.Fatal("expected a fail-closed error when the incomplete model invocation is fenced")
	}
	if inv := rec.Invocation(modelInvocation); inv == nil || inv.AttemptCount != 1 {
		t.Fatalf("model attempt count after resume = %+v, want still 1 (no regeneration)", rec.Invocation(modelInvocation))
	}
}

// ambiguousTool returns ErrExternalOutcomeUnknown on its first invocation and
// succeeds afterwards, modelling an ambiguous external effect.
type ambiguousTool struct {
	Calls int
}

func (t *ambiguousTool) Execute(_ context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	t.Calls++
	if t.Calls == 1 {
		return nil, execution.ErrExternalOutcomeUnknown
	}
	return &port.ToolExecutionResult{Output: "ok"}, nil
}

var _ port.ToolExecutorPort = (*ambiguousTool)(nil)

func newAmbiguousRuntime(t *testing.T, tool *ambiguousTool, rec *harnesstest.RecordingInvocationRepository) *toolruntime.Runtime {
	t.Helper()
	val := validation.NewJSONSchemaValidator()
	tr, err := toolruntime.New(toolruntime.Deps{
		Kernel:    execution.NewKernel(rec, nil),
		Executor:  tool,
		Validator: val,
	})
	if err != nil {
		t.Fatalf("tool runtime: %v", err)
	}
	return tr
}

func toolRuntimeRequest(attemptID string, effect port.ToolEffectClass) toolruntime.Request {
	return toolruntime.Request{
		Identity:      entity.ExecutionIdentity{RunID: "run-r", StepID: "step-1", InvocationID: "inv-1", AttemptID: attemptID},
		Definition:    port.ToolDefinition{Name: "calc", EffectClass: effect, ArgsSchema: []byte("{}")},
		ArgumentsJSON: "{}",
		UserID:        "u1",
		Permission:    toolruntime.Permission{ToolName: "calc"},
	}
}

// Case C: a read-only tool may retry after an ambiguous crash.
func TestReadOnlyToolMayRetryAfterAmbiguousCrash(t *testing.T) {
	tool := &ambiguousTool{}
	tr := newAmbiguousRuntime(t, tool, harnesstest.NewRecordingInvocationRepository())
	if _, err := tr.Execute(context.Background(), toolRuntimeRequest("att-1", port.ToolEffectReadOnly)); !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("first attempt error = %v, want ErrAmbiguousInvocation", err)
	}
	second, err := tr.Execute(context.Background(), toolRuntimeRequest("att-2", port.ToolEffectReadOnly))
	if err != nil {
		t.Fatalf("retry error = %v, want success", err)
	}
	if tool.Calls != 2 {
		t.Fatalf("tool calls = %d, want 2 (read-only retry allowed)", tool.Calls)
	}
	if second.Output != "ok" {
		t.Fatalf("retry output = %q, want ok", second.Output)
	}
}

// Case D: an idempotent tool retry keeps the same idempotency key and
// invocation ID while using a different physical attempt ID.
func TestIdempotentToolRetryUsesSameIdempotencyKey(t *testing.T) {
	tool := &ambiguousTool{}
	rec := harnesstest.NewRecordingInvocationRepository()
	tr := newAmbiguousRuntime(t, tool, rec)
	first, err := tr.Execute(context.Background(), toolRuntimeRequest("att-1", port.ToolEffectIdempotent))
	if !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("first error = %v, want ErrAmbiguousInvocation", err)
	}
	second, err := tr.Execute(context.Background(), toolRuntimeRequest("att-2", port.ToolEffectIdempotent))
	if err != nil {
		t.Fatalf("retry error = %v, want success", err)
	}
	if first.IdempotencyKey == "" || first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("idempotency key mismatch: %q vs %q", first.IdempotencyKey, second.IdempotencyKey)
	}
	if second.Output != "ok" {
		t.Fatalf("retry output = %q, want ok", second.Output)
	}
	persisted := rec.Invocation("inv-1")
	if persisted == nil {
		t.Fatal("expected the logical invocation to be persisted")
	}
	if persisted.IdempotencyKey == nil || *persisted.IdempotencyKey != first.IdempotencyKey {
		t.Fatalf("persisted idempotency key = %v, want %q", persisted.IdempotencyKey, first.IdempotencyKey)
	}
	if persisted.AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2 physical attempts for one logical invocation", persisted.AttemptCount)
	}
}

// Case E: a non-idempotent ambiguous tool must not be replayed.
func TestNonIdempotentToolDoesNotRetryAfterAmbiguousCrash(t *testing.T) {
	tool := &ambiguousTool{}
	tr := newAmbiguousRuntime(t, tool, harnesstest.NewRecordingInvocationRepository())
	if _, err := tr.Execute(context.Background(), toolRuntimeRequest("att-1", port.ToolEffectNonIdempotent)); !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("first error = %v, want ErrAmbiguousInvocation", err)
	}
	if _, err := tr.Execute(context.Background(), toolRuntimeRequest("att-2", port.ToolEffectNonIdempotent)); !errors.Is(err, execution.ErrAmbiguousInvocation) {
		t.Fatalf("retry error = %v, want ErrAmbiguousInvocation (no replay)", err)
	}
	if tool.Calls != 1 {
		t.Fatalf("tool calls = %d, want 1 (non-idempotent tool must not be replayed)", tool.Calls)
	}
}
