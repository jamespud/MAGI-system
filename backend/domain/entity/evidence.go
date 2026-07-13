package entity

import "time"

// EvidenceRecord is an observation collected by a tool (ADR-005). It records
// what the tool returned, NOT the agent's interpretation (which lives in Claim).
type EvidenceRecord struct {
	ID          string
	CaseID      string
	AgentRunID  string
	ToolCallID  string
	ToolName    string
	SourceType  EvidenceSourceType
	SourceURI   *string
	RawContent  string
	Observation string
	Reliability ReliabilityScore
	CollectedBy MagiCode
	CreatedAt   time.Time
}

type EvidenceSourceType string

const (
	EvidenceSourcePlugin    EvidenceSourceType = "plugin"
	EvidenceSourceLocal     EvidenceSourceType = "local"
	EvidenceSourceKnowledge EvidenceSourceType = "knowledge"
	EvidenceSourceWorkflow  EvidenceSourceType = "workflow"
	EvidenceSourceCodeRun   EvidenceSourceType = "coderunner"
)

// ReliabilityScore is a multi-factor heuristic (ADR: not objective truth).
type ReliabilityScore struct {
	Base          float64
	Directness    float64
	Recency       float64
	Corroboration float64
	Extraction    float64
	Final         float64
}
