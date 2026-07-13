package entity

import "time"

// DecisionCase is the aggregate root of a MAGI decision run.
type DecisionCase struct {
	ID              string
	UserID          int64
	Question        string
	Context         string
	Constraints     []Constraint
	Status          CaseStatus
	CurrentPhase    CasePhase
	MaxDebateRounds int
	Deadline        *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CaseStatus string

const (
	CaseStatusDraft            CaseStatus = "DRAFT"
	CaseStatusNormalizing      CaseStatus = "NORMALIZING"
	CaseStatusContextBuilding  CaseStatus = "CONTEXT_BUILDING"
	CaseStatusRetrievingMemory CaseStatus = "RETRIEVING_MEMORY"
	CaseStatusInvestigating    CaseStatus = "INVESTIGATING"
	CaseStatusEvidenceGating   CaseStatus = "EVIDENCE_GATING"
	CaseStatusCollectingVotes  CaseStatus = "COLLECTING_VOTES"
	CaseStatusConsensusCheck   CaseStatus = "CONSENSUS_CHECK"
	CaseStatusResolving        CaseStatus = "RESOLVING"
	CaseStatusGeneratingReport CaseStatus = "GENERATING_REPORT"
	CaseStatusSavingMemory     CaseStatus = "SAVING_MEMORY"
	CaseStatusEvaluating       CaseStatus = "EVALUATING"
	CaseStatusDebating         CaseStatus = "DEBATING"
	CaseStatusReflecting       CaseStatus = "REFLECTING"
	CaseStatusRevoting         CaseStatus = "REVOTING"
	CaseStatusResolved         CaseStatus = "RESOLVED"
	CaseStatusMemoryIndexed    CaseStatus = "MEMORY_INDEXED"
	CaseStatusFailed           CaseStatus = "FAILED"
	CaseStatusCancelled        CaseStatus = "CANCELLED"
	CaseStatusTimedOut         CaseStatus = "TIMED_OUT"
	CaseStatusInsufficientEv   CaseStatus = "INSUFFICIENT_EVIDENCE"
	CaseStatusDeadlocked       CaseStatus = "DEADLOCKED"
)

type CasePhase string

// Constraint is a user-supplied decision constraint.
type Constraint struct {
	Key   string
	Value string
	Hard  bool
}
