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
	CaseStatusInvestigating    CaseStatus = "INVESTIGATING"
	CaseStatusVoting           CaseStatus = "VOTING"
	CaseStatusConsensusCheck   CaseStatus = "CONSENSUS_CHECK"
	CaseStatusResolving        CaseStatus = "RESOLVING"
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
