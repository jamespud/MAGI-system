package execution

import (
	"encoding/json"
	"fmt"

	"github.com/jamespud/magi/backend/domain/entity"
)

const AgentSnapshotV2Version = 2

// TerminationSnapshot persists the loop counters that determine whether a
// resumed run may make another invocation.
type TerminationSnapshot struct {
	GateFail       int   `json:"gate_fail"`
	ConsecToolFail int   `json:"consec_tool_fail"`
	TokenUsed      int64 `json:"token_used"`
	ValidationFail int   `json:"validation_fail"`
	ToolCalls      int   `json:"tool_calls"`
}

// AgentSnapshotV2 is the complete durable state required to resume an agent
// loop without changing the outcome of its next termination decision.
type AgentSnapshotV2 struct {
	Version                   int                 `json:"version"`
	RunID                     string              `json:"run_id"`
	NextStep                  int                 `json:"next_step"`
	Phase                     string              `json:"phase"`
	MessagesJSON              string              `json:"messages_json"`
	Termination               TerminationSnapshot `json:"termination"`
	Usage                     entity.Usage        `json:"usage"`
	Compacted                 bool                `json:"compacted"`
	LedgerJSON                string              `json:"ledger_json"`
	ManifestDigest            string              `json:"manifest_digest"`
	LastCommittedInvocationID string              `json:"last_committed_invocation_id"`
	SummaryJSON               string              `json:"summary_json"`
	ReflectionJSON            string              `json:"reflection_json"`
	PendingResponseJSON       string              `json:"pending_response_json"`
	PendingToolIndex          int                 `json:"pending_tool_index"`
}

func MarshalAgentSnapshotV2(snapshot AgentSnapshotV2) (string, error) {
	if snapshot.Version == 0 {
		snapshot.Version = AgentSnapshotV2Version
	}
	if snapshot.Version != AgentSnapshotV2Version {
		return "", fmt.Errorf("agent snapshot: unsupported version %d", snapshot.Version)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal agent snapshot: %w", err)
	}
	return string(encoded), nil
}

func ParseAgentSnapshotV2(encoded string) (AgentSnapshotV2, error) {
	var snapshot AgentSnapshotV2
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return AgentSnapshotV2{}, fmt.Errorf("parse agent snapshot: %w", err)
	}
	if snapshot.Version != AgentSnapshotV2Version {
		return AgentSnapshotV2{}, fmt.Errorf("agent snapshot: unsupported version %d", snapshot.Version)
	}
	if snapshot.RunID == "" || snapshot.NextStep < 1 || snapshot.MessagesJSON == "" || snapshot.ManifestDigest == "" || snapshot.LedgerJSON == "" {
		return AgentSnapshotV2{}, fmt.Errorf("agent snapshot: incomplete state")
	}
	if snapshot.PendingToolIndex < -1 {
		return AgentSnapshotV2{}, fmt.Errorf("agent snapshot: invalid pending tool index %d", snapshot.PendingToolIndex)
	}
	if snapshot.PendingResponseJSON == "" && snapshot.PendingToolIndex != -1 && snapshot.PendingToolIndex != 0 {
		return AgentSnapshotV2{}, fmt.Errorf("agent snapshot: pending tool index without response")
	}
	return snapshot, nil
}
