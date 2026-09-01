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

func checkpointManifest(cfg *entity.MagiConfig) string {
	tools := make([]string, 0, len(cfg.Tools))
	for _, tool := range cfg.Tools {
		tools = append(tools, string(tool.Source)+":"+tool.ToolName)
	}
	return execution.FreezeManifest(entity.RunEnvironment{ModelName: cfg.Model.ModelName, ModelBaseURL: cfg.Model.BaseURL, Tools: tools, ConfigVersion: cfg.Version}).ManifestDigest
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
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-counters", NextStep: 1, Phase: "gather", MessagesJSON: checkpointMessagesJSON(t), Termination: execution.TerminationSnapshot{GateFail: 1, ConsecToolFail: 2, TokenUsed: 3, ValidationFail: 4, ToolCalls: 5}, LedgerJSON: ledgerJSON, ManifestDigest: checkpointManifest(cfg)})}
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
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-compacted", NextStep: 1, Phase: "vote", MessagesJSON: checkpointCompactedMessagesJSON(t), Termination: execution.TerminationSnapshot{TokenUsed: 50}, Compacted: true, ManifestDigest: checkpointManifest(cfg)})}
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
	repo := &failingCheckpointRepo{load: checkpointState(t, execution.AgentSnapshotV2{RunID: "run-ledger", NextStep: 1, Phase: "vote", MessagesJSON: checkpointMessagesJSON(t), LedgerJSON: ledgerJSON, ManifestDigest: checkpointManifest(cfg)})}
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
