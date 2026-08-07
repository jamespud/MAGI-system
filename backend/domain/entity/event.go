package entity

import (
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

var eventSequence uint64

// MagiEvent is the unified domain event for trace/audit/replay/SSE (ADR-008).
type MagiEvent struct {
	ID        string
	CaseID    string
	RunID     string
	AgentCode *MagiCode
	Type      EventType
	Payload   json.RawMessage
	Timestamp time.Time
}

type EventType string

const (
	EventCaseCreated         EventType = "CASE_CREATED"
	EventTaskNormalized      EventType = "TASK_NORMALIZED"
	EventMemoryRetrieved     EventType = "MEMORY_RETRIEVED"
	EventAgentStarted        EventType = "AGENT_STARTED"
	EventModelRequested      EventType = "MODEL_REQUESTED"
	EventModelResponded      EventType = "MODEL_RESPONDED"
	EventToolCallRequested   EventType = "TOOL_CALL_REQUESTED"
	EventToolCallValidated   EventType = "TOOL_CALL_VALIDATED"
	EventToolCallStarted     EventType = "TOOL_CALL_STARTED"
	EventToolCallCompleted   EventType = "TOOL_CALL_COMPLETED"
	EventToolCallFailed      EventType = "TOOL_CALL_FAILED"
	EventEvidenceCreated     EventType = "EVIDENCE_CREATED"
	EventClaimCreated        EventType = "CLAIM_CREATED"
	EventClaimContradiction  EventType = "CLAIM_CONTRADICTION_DECLARED"
	EventEvidenceGatePassed  EventType = "EVIDENCE_GATE_PASSED"
	EventEvidenceGateFailed  EventType = "EVIDENCE_GATE_FAILED"
	EventVoteSubmitted       EventType = "VOTE_SUBMITTED"
	EventConsensusEvaluated  EventType = "CONSENSUS_EVALUATED"
	EventDebateStarted       EventType = "DEBATE_STARTED"
	EventReflectionSubmitted EventType = "REFLECTION_SUBMITTED"
	EventRevoteSubmitted     EventType = "REVOTE_SUBMITTED"
	EventResolutionCreated   EventType = "RESOLUTION_CREATED"
	EventMemoryIndexed       EventType = "MEMORY_INDEXED"
	EventCaseCompleted       EventType = "CASE_COMPLETED"
	EventCaseFailed          EventType = "CASE_FAILED"
)

// NewEvent constructs a MagiEvent with a unique ID and JSON-serialized payload.
// ID is "<caseID>-<unixNano>-<sequence>". UnixNano alone can collide when
// events are created in the same clock tick; the process-wide sequence makes
// IDs unique even under concurrent publication.
// A nil payload yields a nil RawMessage (no empty `{}`).
func NewEvent(caseID, runID string, agentCode *MagiCode, et EventType, payload any) MagiEvent {
	now := time.Now()
	sequence := atomic.AddUint64(&eventSequence, 1)
	ev := MagiEvent{
		ID:        fmt.Sprintf("%s-%d-%d", caseID, now.UnixNano(), sequence),
		CaseID:    caseID,
		RunID:     runID,
		AgentCode: agentCode,
		Type:      et,
		Timestamp: now,
	}
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			ev.Payload = b
		}
	}
	return ev
}
