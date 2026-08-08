package evaluation_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEvaluationService_Evaluate(t *testing.T) {
	svc := evaluation.NewService()
	results := []*runtime.LoopResult{
		{Status: runtime.LoopStatusCompleted},
		{Status: runtime.LoopStatusCompleted},
	}
	ev, err := svc.Evaluate(context.Background(), results, 1, entity.ConsensusStrongApproval)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil evaluation")
	}
	if !ev.FirstRoundConsensus {
		t.Fatal("expected first-round consensus")
	}
}

func TestEvaluationService_EvaluateCaseFromRepository(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRepository(db)
	if err := repo.AgentRunRepo().Create(context.Background(), &entity.AgentRun{
		ID: "run-1", CaseID: "case-1", Status: entity.AgentRunStatusMaxSteps,
		Usage: &entity.Usage{TotalTokens: 120},
	}); err != nil {
		t.Fatalf("agent run: %v", err)
	}
	if err := repo.EvidenceRepo().Create(context.Background(), &entity.EvidenceRecord{
		ID: "ev-1", CaseID: "case-1", SourceType: entity.EvidenceSourcePlugin,
		Reliability: entity.ReliabilityScore{Final: 0.8},
	}); err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if err := repo.ToolCallRepo().Create(context.Background(), &entity.ToolCall{
		ID: "tc-1", AgentRunID: "run-1", Valid: true,
	}); err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if err := repo.ResolutionRepo().Create(context.Background(), &entity.Resolution{
		ID: "res-1", CaseID: "case-1",
		Consensus: entity.ConsensusResult{Round: 1, Outcome: entity.ConsensusStrongApproval},
	}); err != nil {
		t.Fatalf("resolution: %v", err)
	}
	ev, err := evaluation.NewService(evaluation.WithRepository(repo)).EvaluateCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("evaluate case: %v", err)
	}
	if ev.EvidenceCount != 1 || ev.TotalToolCalls != 1 || ev.MaxStepsExceeded != 1 ||
		ev.TotalTokens != 120 || !ev.FirstRoundConsensus {
		t.Fatalf("evaluation: %+v", ev)
	}
}
