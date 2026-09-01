package runtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/validation"
)

type failingCheckpointRepo struct {
	load    *entity.AgentState
	loadErr error
	saveErr error
	loads   int
	saves   int
}

func (r *failingCheckpointRepo) Save(_ context.Context, _ *entity.AgentState) error {
	r.saves++
	return r.saveErr
}

func (r *failingCheckpointRepo) Load(_ context.Context, _ string) (*entity.AgentState, error) {
	r.loads++
	return r.load, r.loadErr
}

var _ port.CheckpointRepository = (*failingCheckpointRepo)(nil)

type memoryCheckpointRepo struct {
	state      *entity.AgentState
	history    []*entity.AgentState
	saves      int
	failSaveAt int
	saveErr    error
}

func (r *memoryCheckpointRepo) Save(_ context.Context, state *entity.AgentState) error {
	r.saves++
	if r.failSaveAt == r.saves {
		return r.saveErr
	}
	copy := *state
	r.state = &copy
	r.history = append(r.history, &copy)
	return nil
}

func (r *memoryCheckpointRepo) Load(_ context.Context, _ string) (*entity.AgentState, error) {
	if r.state == nil {
		return nil, nil
	}
	copy := *r.state
	return &copy, nil
}

var _ port.CheckpointRepository = (*memoryCheckpointRepo)(nil)

type countingToolExecutor struct{ calls int }

func (e *countingToolExecutor) Execute(_ context.Context, _ port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	e.calls++
	return &port.ToolExecutionResult{Output: "3"}, nil
}

func checkpointLoop(t *testing.T, repo port.CheckpointRepository, model *scriptedChatModel) *runtime.AgentLoop {
	t.Helper()
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &stubModelPort{m: model},
		Validator:      validation.NewJSONSchemaValidator(),
		Gen:            validation.NewReflectSchemaGenerator(),
		CheckpointRepo: repo,
	})
	if err != nil {
		t.Fatalf("new loop: %v", err)
	}
	return loop
}

func checkpointToolLoop(t *testing.T, repo port.CheckpointRepository, model *scriptedChatModel, exec port.ToolExecutorPort) *runtime.AgentLoop {
	t.Helper()
	gen := validation.NewReflectSchemaGenerator()
	calcSchema, err := gen.FromStruct(calcArgs{})
	if err != nil {
		t.Fatalf("calc schema: %v", err)
	}
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort:      &stubModelPort{m: model},
		ToolReg:        &stubToolReg{defs: []port.ToolDefinition{{Name: "calc", Desc: "add", ArgsSchema: calcSchema, Source: entity.ToolSourceLocal, Binding: entity.ToolBinding{Source: entity.ToolSourceLocal, ToolName: "calc"}}}},
		ToolExec:       exec,
		Validator:      validation.NewJSONSchemaValidator(),
		Gen:            gen,
		CheckpointRepo: repo,
	})
	if err != nil {
		t.Fatalf("new tool loop: %v", err)
	}
	return loop
}

func checkpointState(t *testing.T, snapshot execution.AgentSnapshotV2) *entity.AgentState {
	t.Helper()
	if snapshot.LedgerJSON == "" {
		var err error
		snapshot.LedgerJSON, err = evidence.MarshalLedger(evidence.NewEvidenceLedger("", snapshot.RunID, "melchior"))
		if err != nil {
			t.Fatalf("marshal empty ledger: %v", err)
		}
	}
	encoded, err := execution.MarshalAgentSnapshotV2(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	return &entity.AgentState{RunID: snapshot.RunID, SnapshotVersion: execution.AgentSnapshotV2Version, SnapshotJSON: encoded, ManifestDigest: snapshot.ManifestDigest}
}

func checkpointManifest(t *testing.T, cfg *entity.MagiConfig) string {
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
	prompt := runtime.BuildAgentSystemPrompt(cfg, summarySchema, voteSchema, reflectionSchema, nil, false)
	return runtime.CheckpointManifest(cfg, &runtime.AgentContext{}, prompt)
}

func TestCheckpointSaveFailureStopsBeforeNextInvocation(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	repo := &failingCheckpointRepo{saveErr: errors.New("checkpoint unavailable")}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON()), finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-save", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil || model.calls != 0 || repo.saves != 1 {
		t.Fatalf("err=%v calls=%d saves=%d, want failed checkpoint before invocation", err, model.calls, repo.saves)
	}
}

func TestCheckpointLoadFailureDoesNotRestartFromScratch(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, &failingCheckpointRepo{loadErr: errors.New("checkpoint unavailable")}, model)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-load", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil || model.calls != 0 {
		t.Fatalf("err=%v calls=%d, want load failure before invocation", err, model.calls)
	}
}

func TestCheckpointMissingStartsNewRun(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	repo := &failingCheckpointRepo{}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON()), finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	result, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-new", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil || result.Status != runtime.LoopStatusCompleted || model.calls != 2 || repo.loads != 1 || repo.saves == 0 {
		t.Fatalf("result=%+v err=%v calls=%d loads=%d saves=%d, want new run after missing checkpoint", result, err, model.calls, repo.loads, repo.saves)
	}
}

func TestCheckpointIncompleteLegacyStateDoesNotRestartFromScratch(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	repo := &failingCheckpointRepo{load: &entity.AgentState{RunID: "run-incomplete"}}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON()), finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-incomplete", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil || model.calls != 0 {
		t.Fatalf("err=%v calls=%d, want incomplete checkpoint to stop before invocation", err, model.calls)
	}
}

func TestNilCheckpointRepoAllowsStatelessExecution(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON()), finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, nil, model)

	result, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-stateless", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil || result.Status != runtime.LoopStatusCompleted || model.calls != 2 {
		t.Fatalf("result=%+v err=%v calls=%d, want stateless execution", result, err, model.calls)
	}
}

func TestCheckpointRestoresTerminationCounters(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	cfg.LoopPolicy.MaxGateFailures = 2
	cfg.EvidenceStandard.CustomRules = []entity.EvidenceRule{{Code: "missing"}}
	ledger := evidence.NewEvidenceLedger("case-1", "run-counters", "melchior")
	ledgerJSON, err := evidence.MarshalLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-counters", NextStep: 1, Phase: "gather", MessagesJSON: checkpointMessagesJSON(t), Termination: execution.TerminationSnapshot{GateFail: 1, ConsecToolFail: 2, TokenUsed: 3, ValidationFail: 4, ToolCalls: 5}, LedgerJSON: ledgerJSON, ManifestDigest: checkpointManifest(t, cfg)})}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON())}}
	loop := checkpointLoop(t, repo, model)

	result, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "case-1", RunID: "run-counters", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if result == nil || err == nil || result.Status != runtime.LoopStatusGateFailed || model.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want restored gate counter to terminate", result, err, model.calls)
	}
}

func TestCheckpointRestoresCompactionState(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	cfg.LoopPolicy.TokenBudget = 100
	cfg.LoopPolicy.TokenCompactionThreshold = 0.5
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-compacted", NextStep: 1, Phase: "vote", MessagesJSON: checkpointCompactedMessagesJSON(t), Termination: execution.TerminationSnapshot{TokenUsed: 50}, Compacted: true, ManifestDigest: checkpointManifest(t, cfg), SummaryJSON: summaryJSON()})}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	result, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-compacted", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err != nil || result.Status != runtime.LoopStatusCompleted || model.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, model.calls)
	}
}

func TestCheckpointRestoresEvidenceLedger(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	ledger := evidence.NewEvidenceLedger("case-1", "run-ledger", "melchior")
	record := ledger.Record("tool-1", "web_search", "mcp", "https://example.test", "observation", entity.ReliabilityScore{Final: 0.95})
	ledger.RecordClaim("claim", []string{record.ID}, nil)
	ledgerJSON, err := evidence.MarshalLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-ledger", NextStep: 1, Phase: "vote", MessagesJSON: checkpointMessagesJSON(t), LedgerJSON: ledgerJSON, ManifestDigest: checkpointManifest(t, cfg), SummaryJSON: summaryJSON("EV-001")})}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	result, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{CaseID: "case-1", RunID: "run-ledger", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if result == nil || err != nil || len(result.Ledger.List()) != 1 || result.Ledger.List()[0].ID != record.ID || len(result.Ledger.ListClaims()) != 1 {
		t.Fatalf("result=%+v err=%v ledger=%+v claims=%+v", result, err, result.Ledger.List(), result.Ledger.ListClaims())
	}
}

func TestCheckpointRejectsManifestMismatch(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-manifest", NextStep: 1, Phase: "vote", MessagesJSON: checkpointMessagesJSON(t), ManifestDigest: "different-manifest"})}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	loop := checkpointLoop(t, repo, model)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-manifest", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if !errors.Is(err, execution.ErrManifestMismatch) || model.calls != 0 {
		t.Fatalf("err=%v calls=%d, want manifest mismatch before invocation", err, model.calls)
	}
}

func TestCheckpointRestartRestoresSummaryForDefaultRolePolicy(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	cfg.Code = "balthasar"
	cfg.RolePolicy = entity.DefaultRolePolicy("balthasar")
	cfg.LoopPolicy.MaxSteps = 2
	repo := &memoryCheckpointRepo{}
	firstLoop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(riskSummaryJSON(0.20)),
	}}, &countingToolExecutor{})
	actx := &runtime.AgentContext{RunID: "run-role-restart", Task: entity.DecisionTask{CanonicalQuestion: "deploy"}}

	if _, err := firstLoop.Run(context.Background(), cfg, actx); !errors.Is(err, runtime.ErrMaxSteps) {
		t.Fatalf("first run error = %v, want ErrMaxSteps", err)
	}

	resumedCfg := *cfg
	resumedCfg.LoopPolicy.MaxSteps = 3
	resumedModel := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	secondLoop := checkpointToolLoop(t, repo, resumedModel, &countingToolExecutor{})
	result, err := secondLoop.Run(context.Background(), &resumedCfg, actx)
	if err != nil || result.Status != runtime.LoopStatusCompleted || result.Summary == nil || result.Vote == nil || resumedModel.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want resumed role-policy vote", result, err, resumedModel.calls)
	}
}

func TestCheckpointReconstructsMissingV2SummaryForDefaultRolePolicy(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	cfg.Code = "balthasar"
	cfg.RolePolicy = entity.DefaultRolePolicy("balthasar")
	cfg.LoopPolicy.MaxSteps = 2
	repo := &memoryCheckpointRepo{}
	firstLoop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(riskSummaryJSON(0.20)),
	}}, &countingToolExecutor{})
	actx := &runtime.AgentContext{RunID: "run-role-missing-v2", Task: entity.DecisionTask{CanonicalQuestion: "deploy"}}

	if _, err := firstLoop.Run(context.Background(), cfg, actx); !errors.Is(err, runtime.ErrMaxSteps) {
		t.Fatalf("first run error = %v, want ErrMaxSteps", err)
	}
	preFixSnapshot, err := execution.ParseAgentSnapshotV2(repo.state.SnapshotJSON)
	if err != nil {
		t.Fatalf("parse saved snapshot: %v", err)
	}
	preFixSnapshot.SummaryJSON = ""
	repo.state = checkpointState(t, preFixSnapshot)

	resumedCfg := *cfg
	resumedCfg.LoopPolicy.MaxSteps = 3
	resumedModel := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	secondLoop := checkpointToolLoop(t, repo, resumedModel, &countingToolExecutor{})
	result, err := secondLoop.Run(context.Background(), &resumedCfg, actx)
	if err != nil || result.Status != runtime.LoopStatusCompleted || result.Summary == nil || result.Vote == nil || resumedModel.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want compatible pre-fix V2 vote resume", result, err, resumedModel.calls)
	}
}

func TestLegacyCheckpointReconstructsSummaryForDefaultRolePolicy(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	cfg.Code = "balthasar"
	cfg.RolePolicy = entity.DefaultRolePolicy("balthasar")
	cfg.LoopPolicy.MaxSteps = 2
	repo := &memoryCheckpointRepo{}
	firstLoop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
		finalMsg(riskSummaryJSON(0.20)),
	}}, &countingToolExecutor{})
	actx := &runtime.AgentContext{RunID: "run-role-legacy", Task: entity.DecisionTask{CanonicalQuestion: "deploy"}}

	if _, err := firstLoop.Run(context.Background(), cfg, actx); !errors.Is(err, runtime.ErrMaxSteps) {
		t.Fatalf("first run error = %v, want ErrMaxSteps", err)
	}
	saved := repo.state
	repo.state = &entity.AgentState{
		RunID: saved.RunID, MessagesJSON: saved.MessagesJSON, StepCount: saved.StepCount,
		TokenUsed: saved.TokenUsed, Phase: saved.Phase,
	}

	resumedCfg := *cfg
	resumedCfg.LoopPolicy.MaxSteps = 3
	resumedModel := &scriptedChatModel{responses: []*schema.Message{finalMsg(voteJSON("correctness"))}}
	secondLoop := checkpointToolLoop(t, repo, resumedModel, &countingToolExecutor{})
	result, err := secondLoop.Run(context.Background(), &resumedCfg, actx)
	if err != nil || result.Status != runtime.LoopStatusCompleted || result.Summary == nil || result.Vote == nil || resumedModel.calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want compatible legacy vote resume", result, err, resumedModel.calls)
	}
}

func TestCheckpointSaveFailureStopsBeforeCompactionInvocation(t *testing.T) {
	cfg := evidenceCfg(0, 0)
	cfg.LoopPolicy.TokenBudget = 100
	cfg.LoopPolicy.TokenCompactionThreshold = 0.5
	repo := &failingCheckpointRepo{
		load: checkpointState(t, execution.AgentSnapshotV2{
			RunID: "run-compaction-save", NextStep: 1, Phase: "vote",
			MessagesJSON:   checkpointCompactedMessagesJSON(t),
			Termination:    execution.TerminationSnapshot{TokenUsed: 50},
			ManifestDigest: checkpointManifest(t, cfg),
			SummaryJSON:    summaryJSON(),
		}),
		saveErr: errors.New("checkpoint unavailable"),
	}
	model := &scriptedChatModel{responses: []*schema.Message{finalMsg("compacted")}}
	loop := checkpointLoop(t, repo, model)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-compaction-save", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil || model.calls != 0 || repo.saves != 1 {
		t.Fatalf("err=%v calls=%d saves=%d, want save failure before compaction", err, model.calls, repo.saves)
	}
}

func TestCheckpointSaveFailureAfterModelResponseStopsBeforeToolInvocation(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	repo := &memoryCheckpointRepo{failSaveAt: 2, saveErr: errors.New("checkpoint unavailable")}
	exec := &countingToolExecutor{}
	model := &scriptedChatModel{responses: []*schema.Message{callMsg("c1", "calc", `{"a":1,"b":2}`)}}
	loop := checkpointToolLoop(t, repo, model, exec)

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: "run-model-save", Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if err == nil || model.calls != 1 || exec.calls != 0 {
		t.Fatalf("err=%v model_calls=%d tool_calls=%d, want save failure before tool invocation", err, model.calls, exec.calls)
	}
}

func TestCheckpointResumePendingToolDoesNotDuplicateAssistantToolCall(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.MaxSteps = 3
	repo := &memoryCheckpointRepo{failSaveAt: 4, saveErr: errors.New("checkpoint unavailable")}
	firstExec := &countingToolExecutor{}
	firstLoop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{
		callMsg("c1", "calc", `{"a":1,"b":2}`),
	}}, firstExec)
	actx := &runtime.AgentContext{RunID: "run-pending-tool", Task: entity.DecisionTask{CanonicalQuestion: "q"}}

	if _, err := firstLoop.Run(context.Background(), cfg, actx); err == nil || firstExec.calls != 1 {
		t.Fatalf("first run err=%v tool_calls=%d, want post-tool checkpoint failure", err, firstExec.calls)
	}
	pendingSnapshot, err := execution.ParseAgentSnapshotV2(repo.state.SnapshotJSON)
	if err != nil || pendingSnapshot.LastCommittedInvocationID == "" || pendingSnapshot.PendingResponseJSON == "" || pendingSnapshot.NextStep != 1 {
		t.Fatalf("pending snapshot=%+v err=%v, want committed response at step 1", pendingSnapshot, err)
	}
	repo.failSaveAt = 0
	secondExec := &countingToolExecutor{}
	secondLoop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{
		finalMsg(summaryJSON("EV-001")), finalMsg(voteJSON("correctness")),
	}}, secondExec)
	result, err := secondLoop.Run(context.Background(), cfg, actx)
	if err != nil || result.Status != runtime.LoopStatusCompleted || secondExec.calls != 1 {
		t.Fatalf("resume result=%+v err=%v tool_calls=%d", result, err, secondExec.calls)
	}

	snapshot, err := execution.ParseAgentSnapshotV2(repo.state.SnapshotJSON)
	if err != nil {
		t.Fatalf("parse saved snapshot: %v", err)
	}
	var messages []*schema.Message
	if err := json.Unmarshal([]byte(snapshot.MessagesJSON), &messages); err != nil {
		t.Fatalf("unmarshal saved messages: %v", err)
	}
	toolCallMessages := 0
	for _, message := range messages {
		if len(message.ToolCalls) > 0 {
			toolCallMessages++
		}
	}
	if toolCallMessages != 1 {
		t.Fatalf("tool call messages=%d, want one persisted assistant tool call", toolCallMessages)
	}
}

func TestCheckpointTracksCommittedToolInvocationAtEachRecoverableBoundary(t *testing.T) {
	cfg := evidenceCfg(1, 0)
	cfg.LoopPolicy.MaxSteps = 1
	repo := &memoryCheckpointRepo{}
	toolResponse := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "c1", Type: "function", Function: schema.FunctionCall{Name: "calc", Arguments: `{"a":1,"b":2}`}},
		{ID: "c2", Type: "function", Function: schema.FunctionCall{Name: "calc", Arguments: `{"a":3,"b":4}`}},
	})
	exec := &countingToolExecutor{}
	loop := checkpointToolLoop(t, repo, &scriptedChatModel{responses: []*schema.Message{toolResponse}}, exec)
	runID := "run-committed-tools"

	_, err := loop.Run(context.Background(), cfg, &runtime.AgentContext{RunID: runID, Task: entity.DecisionTask{CanonicalQuestion: "q"}})
	if !errors.Is(err, runtime.ErrMaxSteps) || exec.calls != 2 {
		t.Fatalf("err=%v tool_calls=%d, want two completed tools before max steps", err, exec.calls)
	}

	stepID := execution.NewStepID(runID, 1)
	wantFirst := execution.NewInvocationID(stepID, execution.InvocationTool, 0)
	wantLast := execution.NewInvocationID(stepID, execution.InvocationTool, 1)
	var beforeSecond execution.AgentSnapshotV2
	for _, state := range repo.history {
		snapshot, parseErr := execution.ParseAgentSnapshotV2(state.SnapshotJSON)
		if parseErr != nil {
			t.Fatalf("parse saved snapshot: %v", parseErr)
		}
		if snapshot.PendingToolIndex == 1 {
			beforeSecond = snapshot
			break
		}
	}
	if beforeSecond.LastCommittedInvocationID != wantFirst || beforeSecond.NextStep != 1 {
		t.Fatalf("before second tool snapshot=%+v, want committed=%q next_step=1", beforeSecond, wantFirst)
	}

	finalSnapshot, err := execution.ParseAgentSnapshotV2(repo.state.SnapshotJSON)
	if err != nil {
		t.Fatalf("parse final snapshot: %v", err)
	}
	if finalSnapshot.LastCommittedInvocationID != wantLast || finalSnapshot.NextStep != 2 || finalSnapshot.PendingResponseJSON != "" {
		t.Fatalf("final snapshot=%+v, want committed=%q next_step=2 with no pending response", finalSnapshot, wantLast)
	}
}

func TestCheckpointManifestRejectsChangedInvocationEnvironment(t *testing.T) {
	base := evidenceCfg(0, 0)
	base.Model.ModelName = "model-a"
	base.Model.Params = &entity.LLMParams{MaxTokens: 128}
	base.Persona = "first prompt"
	base.Tools = []entity.ToolBinding{{Source: entity.ToolSourceMCP, Server: "market-data"}}

	changes := []struct {
		name   string
		mutate func(*entity.MagiConfig)
	}{
		{name: "model params", mutate: func(cfg *entity.MagiConfig) { cfg.Model.Params.MaxTokens = 256 }},
		{name: "tool binding", mutate: func(cfg *entity.MagiConfig) { cfg.Tools[0].Server = "different-server" }},
		{name: "prompt workflow", mutate: func(cfg *entity.MagiConfig) { cfg.Persona = "different prompt" }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			repo := &memoryCheckpointRepo{}
			firstLoop := checkpointLoop(t, repo, &scriptedChatModel{})
			actx := &runtime.AgentContext{RunID: "run-manifest-" + tc.name, Task: entity.DecisionTask{CanonicalQuestion: "q"}}
			if _, err := firstLoop.Run(context.Background(), base, actx); err == nil {
				t.Fatal("first run unexpectedly completed")
			}

			changed := *base
			changed.Tools = append([]entity.ToolBinding(nil), base.Tools...)
			params := *base.Model.Params
			changed.Model.Params = &params
			tc.mutate(&changed)
			resumedModel := &scriptedChatModel{responses: []*schema.Message{finalMsg(summaryJSON())}}
			secondLoop := checkpointLoop(t, repo, resumedModel)
			_, err := secondLoop.Run(context.Background(), &changed, actx)
			if !errors.Is(err, execution.ErrManifestMismatch) || resumedModel.calls != 0 {
				t.Fatalf("err=%v calls=%d, want manifest rejection before invocation", err, resumedModel.calls)
			}
		})
	}
}

func checkpointMessagesJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal([]*schema.Message{schema.SystemMessage("system"), schema.UserMessage("q")})
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	return string(encoded)
}

func checkpointCompactedMessagesJSON(t *testing.T) string {
	t.Helper()
	encoded, err := json.Marshal([]*schema.Message{
		schema.SystemMessage("system"), schema.UserMessage("q"), schema.AssistantMessage("a", nil),
		schema.UserMessage("b"), schema.AssistantMessage("c", nil), schema.UserMessage("d"),
	})
	if err != nil {
		t.Fatalf("marshal compacted messages: %v", err)
	}
	return string(encoded)
}
