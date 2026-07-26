package memory

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestRenderDocumentContainsAllFields(t *testing.T) {
	proj := &entity.CaseMemoryProjection{
		CaseID:          "case-1",
		QuestionSummary: "Should we rewrite backend in Rust?",
		ContextSummary:  "Team has 2 Rust engineers.",
		KeyEvidence: []entity.MemoryEvidence{
			{EvidenceID: "ev-1", Observation: "Rust latency 2x lower", Reliability: 0.8},
		},
		KeyClaims: []entity.MemoryClaim{
			{ClaimID: "cl-1", Statement: "Rust improves latency"},
		},
		Votes: []entity.MemoryVote{
			{MagiCode: "melchior", Decision: "APPROVE", Confidence: 0.7},
		},
		Resolution: "Approve with conditions",
		Outcome:    &entity.CaseOutcome{Status: "RESOLVED", Learned: "Latency matters"},
	}

	doc := RenderDocument(proj)
	for _, want := range []string{
		"Should we rewrite backend in Rust?",
		"Team has 2 Rust engineers.",
		"Rust latency 2x lower",
		"Rust improves latency",
		"melchior",
		"Approve with conditions",
		"Latency matters",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("RenderDocument missing %q\ngot:\n%s", want, doc)
		}
	}
}

func TestRenderDocumentNoTruncation(t *testing.T) {
	long := strings.Repeat("x", 5000)
	proj := &entity.CaseMemoryProjection{
		CaseID:          "case-2",
		QuestionSummary: long,
	}
	doc := RenderDocument(proj)
	if !strings.Contains(doc, long) {
		t.Error("RenderDocument truncated the question; full content must be preserved for indexing")
	}
}
