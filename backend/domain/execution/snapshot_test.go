package execution_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/execution"
)

func TestAgentSnapshotV2RoundTrip(t *testing.T) {
	ledger := evidence.NewEvidenceLedger("case-1", "run-1", "melchior")
	record := ledger.Record("tool-1", "web_search", "mcp", "https://example.test", "observation", entity.ReliabilityScore{Final: 0.91})
	ledger.RecordClaim("claim", []string{record.ID}, nil)

	ledgerJSON, err := evidence.MarshalLedger(ledger)
	if err != nil {
		t.Fatalf("marshal ledger: %v", err)
	}
	snapshotJSON, err := execution.MarshalAgentSnapshotV2(execution.AgentSnapshotV2{
		RunID:                     "run-1",
		NextStep:                  4,
		Phase:                     "vote",
		MessagesJSON:              `[{"role":"user","content":"question"}]`,
		Termination:               execution.TerminationSnapshot{GateFail: 1, ConsecToolFail: 2, TokenUsed: 13, ValidationFail: 3, ToolCalls: 4},
		Usage:                     entity.Usage{PromptTokens: 5, CompletionTokens: 8, TotalTokens: 13, CostUSD: 0.42},
		Compacted:                 true,
		LedgerJSON:                ledgerJSON,
		ManifestDigest:            "manifest-a",
		LastCommittedInvocationID: "invocation-3",
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	got, err := execution.ParseAgentSnapshotV2(snapshotJSON)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if got.Version != execution.AgentSnapshotV2Version || got.NextStep != 4 || !got.Compacted || got.Termination.ToolCalls != 4 || got.Usage.TotalTokens != 13 || got.ManifestDigest != "manifest-a" || got.LastCommittedInvocationID != "invocation-3" {
		t.Fatalf("snapshot lost state: %+v", got)
	}
	restored, err := evidence.RestoreLedger(got.LedgerJSON)
	if err != nil {
		t.Fatalf("restore ledger: %v", err)
	}
	if records := restored.List(); len(records) != 1 || records[0].ID != record.ID || records[0].Reliability.Final != 0.91 {
		t.Fatalf("restored records: %+v", records)
	}
	if claims := restored.ListClaims(); len(claims) != 1 || claims[0].ID != "CL-001" || claims[0].Supports[0] != record.ID {
		t.Fatalf("restored claims: %+v", claims)
	}
}
