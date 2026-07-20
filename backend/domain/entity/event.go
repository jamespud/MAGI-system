package entity

import (
	"encoding/json"
	"time"
)

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
