package magi_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
)

type stubInvestigator struct {
	result *magi.DelegateResult
	err    error
}

func (s *stubInvestigator) Investigate(ctx context.Context, question, background string) (*magi.DelegateResult, error) {
	return s.result, s.err
}

func TestDelegateToolExecutor_RunsSubInvestigation(t *testing.T) {
	investigator := &stubInvestigator{result: &magi.DelegateResult{
		Question: "q", Status: "completed",
		Evidence: []map[string]any{
			{"id": "EV-SUB-1", "tool": "web_search", "source": "https://a", "observation": "found", "reliability": 0.9},
		},
	}}
	exec, err := magi.NewDelegateToolExecutor(investigator)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.DelegateToolName, ArgumentsJSON: `{"question":"investigate X"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Question      string           `json:"question"`
		Status        string           `json:"status"`
		Evidence      []map[string]any `json:"evidence"`
		EvidenceCount int              `json:"evidence_count"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != "completed" || out.EvidenceCount != 1 || out.Evidence[0]["id"] != "EV-SUB-1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestDelegateToolExecutor_RequiresQuestionAndInvestigator(t *testing.T) {
	if _, err := magi.NewDelegateToolExecutor(nil); err == nil {
		t.Fatal("expected missing investigator error")
	}
	exec, err := magi.NewDelegateToolExecutor(&stubInvestigator{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName: magi.DelegateToolName, ArgumentsJSON: `{}`,
	}); err == nil || !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("expected question error, got %v", err)
	}
}

func TestDelegateToolExecutor_RunsParallelSubInvestigations(t *testing.T) {
	var active, maxActive int32
	investigator := &trackingInvestigator{active: &active, maxActive: &maxActive}
	exec, err := magi.NewDelegateToolExecutor(investigator)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res, err := exec.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName:      magi.DelegateToolName,
		ArgumentsJSON: `{"questions":[{"question":"q1"},{"question":"q2"},{"question":"q3"}]}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Results       []magi.DelegateResult `json:"results"`
		EvidenceCount int                   `json:"evidence_count"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Results) != 3 || out.EvidenceCount != 3 {
		t.Fatalf("results=%d evidence=%d", len(out.Results), out.EvidenceCount)
	}
	if maxActive < 2 {
		t.Fatalf("expected concurrency >= 2, got %d", maxActive)
	}
}

type trackingInvestigator struct {
	active    *int32
	maxActive *int32
}

func (s *trackingInvestigator) Investigate(ctx context.Context, question, background string) (*magi.DelegateResult, error) {
	cur := atomic.AddInt32(s.active, 1)
	for {
		observed := atomic.LoadInt32(s.maxActive)
		if cur <= observed || atomic.CompareAndSwapInt32(s.maxActive, observed, cur) {
			break
		}
	}
	defer atomic.AddInt32(s.active, -1)
	return &magi.DelegateResult{Question: question, Status: "completed", Evidence: []map[string]any{
		{"id": "EV-" + question, "tool": "web_search", "observation": "found"},
	}}, nil
}

type stubMagiRuntime struct {
	result *runtime.LoopResult
	err    error
}

func (s *stubMagiRuntime) Run(ctx context.Context, cfg *entity.MagiConfig, actx *runtime.AgentContext) (*runtime.LoopResult, error) {
	return s.result, s.err
}

func TestLoopSubInvestigator_CollectsEvidence(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("case-sub", "run-sub", "melchior")
	source := "https://docs.example/x"
	ledger.Record("tc-1", "web_search", "local", source, "sub observation", entity.ReliabilityScore{Final: 0.85})
	investigator, err := magi.NewLoopSubInvestigator(&stubMagiRuntime{
		result: &runtime.LoopResult{Status: runtime.LoopStatusCompleted, Ledger: ledger},
	}, &entity.MagiConfig{Code: "melchior"})
	if err != nil {
		t.Fatalf("build investigator: %v", err)
	}
	result, err := investigator.Investigate(context.Background(), "sub question", "")
	if err != nil {
		t.Fatalf("investigate: %v", err)
	}
	if result.Status != "completed" || len(result.Evidence) != 1 ||
		result.Evidence[0]["observation"] != "sub observation" || result.Evidence[0]["source"] != source {
		t.Fatalf("result = %+v", result)
	}
}

func TestLoopSubInvestigator_RequiresLoopAndConfig(t *testing.T) {
	if _, err := magi.NewLoopSubInvestigator(nil, &entity.MagiConfig{Code: "m"}); err == nil {
		t.Fatal("expected missing loop error")
	}
	if _, err := magi.NewLoopSubInvestigator(&stubMagiRuntime{}, nil); err == nil {
		t.Fatal("expected missing config error")
	}
}
