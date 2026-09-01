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
		SummaryJSON:               `{"ready":true}`,
		ReflectionJSON:            `{"ready_to_revote":true}`,
		PendingResponseJSON:       `{"role":"assistant","content":"","tool_calls":[{"id":"tool-1","type":"function","function":{"name":"calc","arguments":"{}"}}]}`,
		PendingToolIndex:          0,
	})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	got, err := execution.ParseAgentSnapshotV2(snapshotJSON)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if got.Version != execution.AgentSnapshotV2Version || got.NextStep != 4 || !got.Compacted || got.Termination.ToolCalls != 4 || got.Usage.TotalTokens != 13 || got.ManifestDigest != "manifest-a" || got.LastCommittedInvocationID != "invocation-3" || got.SummaryJSON == "" || got.ReflectionJSON == "" || got.PendingResponseJSON == "" || got.PendingToolIndex != 0 {
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

func TestParseAgentSnapshotV2ValidatesPendingToolIndex(t *testing.T) {
	tests := []struct {
		name            string
		pendingResponse string
		pendingIndex    int
		wantError       bool
	}{
		{name: "no response legacy zero", pendingIndex: 0},
		{name: "no response sentinel", pendingIndex: -1},
		{name: "response not processed", pendingResponse: `{"role":"assistant","content":"pending"}`, pendingIndex: -1},
		{name: "first pending tool", pendingResponse: pendingToolResponseJSON(), pendingIndex: 0},
		{name: "last pending tool", pendingResponse: pendingToolResponseJSON(), pendingIndex: 1},
		{name: "no response positive index", pendingIndex: 1, wantError: true},
		{name: "below sentinel", pendingIndex: -2, wantError: true},
		{name: "response without tools", pendingResponse: `{"role":"assistant","content":"pending"}`, pendingIndex: 0, wantError: true},
		{name: "index equals tool count", pendingResponse: pendingToolResponseJSON(), pendingIndex: 2, wantError: true},
		{name: "index exceeds tool count", pendingResponse: pendingToolResponseJSON(), pendingIndex: 3, wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := execution.MarshalAgentSnapshotV2(execution.AgentSnapshotV2{
				RunID:               "run-pending-index",
				NextStep:            1,
				MessagesJSON:        `[{"role":"user","content":"question"}]`,
				LedgerJSON:          `{"records":[],"claims":[]}`,
				ManifestDigest:      "manifest-a",
				PendingResponseJSON: tc.pendingResponse,
				PendingToolIndex:    tc.pendingIndex,
			})
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}

			_, err = execution.ParseAgentSnapshotV2(encoded)
			if tc.wantError && err == nil {
				t.Fatal("ParseAgentSnapshotV2() error = nil, want malformed pending tool index rejection")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("ParseAgentSnapshotV2() error = %v, want valid pending state", err)
			}
		})
	}
}

func pendingToolResponseJSON() string {
	return `{"role":"assistant","content":"","tool_calls":[{"id":"tool-1","type":"function","function":{"name":"calc","arguments":"{}"}},{"id":"tool-2","type":"function","function":{"name":"calc","arguments":"{}"}}]}`
}
