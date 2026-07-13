package memory_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/memory"
)

func TestBuildProjection_Full(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("c1", "r1", "melchior")
	ledger.Record("tc", "calc", "local", "", "obs1", entity.ReliabilityScore{Final: 0.9})
	ledger.RecordClaim("claim1", []string{"EV-001"}, nil)
	res := &entity.Resolution{FinalReport: "report content", Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval}}
	votes := []*entity.Vote{{Decision: entity.VoteDecisionApprove, Confidence: 90}}

	proj := memory.BuildProjection(&entity.DecisionCase{Question: "q", Context: "ctx"}, res, ledger, votes)
	if proj.QuestionSummary != "q" {
		t.Fatalf("question: %s", proj.QuestionSummary)
	}
	if proj.ContextSummary != "report content" {
		t.Fatalf("context: %s", proj.ContextSummary)
	}
	if len(proj.KeyEvidence) != 1 || proj.KeyEvidence[0].EvidenceID != "EV-001" {
		t.Fatalf("evidence: %+v", proj.KeyEvidence)
	}
	if len(proj.KeyClaims) != 1 {
		t.Fatalf("claims: %d", len(proj.KeyClaims))
	}
	if len(proj.Votes) != 1 || proj.Votes[0].Decision != entity.VoteDecisionApprove {
		t.Fatalf("votes: %+v", proj.Votes)
	}
	if proj.Outcome == nil || proj.Outcome.Status != string(entity.ConsensusStrongApproval) {
		t.Fatalf("outcome: %+v", proj.Outcome)
	}
}

func TestBuildProjection_EmptyResolution(t *testing.T) {
	proj := memory.BuildProjection(&entity.DecisionCase{Question: "q", Context: "ctx"}, nil, nil, nil)
	if proj.ContextSummary != "ctx" {
		t.Fatalf("context: %s", proj.ContextSummary)
	}
}

func TestBuildProjection_Truncation(t *testing.T) {
	long := strings.Repeat("x", 300)
	res := &entity.Resolution{FinalReport: long}
	proj := memory.BuildProjection(&entity.DecisionCase{Question: "q"}, res, nil, nil)
	if len([]rune(proj.ContextSummary)) > 200 {
		t.Fatalf("context not truncated: %d", len([]rune(proj.ContextSummary)))
	}
	if len([]rune(proj.Resolution)) > 500 {
		t.Fatalf("resolution not truncated: %d", len([]rune(proj.Resolution)))
	}
}
