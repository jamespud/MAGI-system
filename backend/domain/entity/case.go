package entity

import "time"

// DecisionCase is the aggregate root of a MAGI decision run.
type DecisionCase struct {
	ID           string
	UserID       int64
	Question     string
	Context      string
	Constraints  []Constraint
	ParentCaseID string // lineage link: this case was forked from ParentCaseID
	Status       CaseStatus
	CurrentPhase CasePhase
	// Pinned/Archived are user-level list management flags (P2 D11): pin keeps a
	// case at the top of the sidebar, archive hides it from active lists.
	Pinned   bool
	Archived bool
	// ExecutionAttempt is set by the durable worker for the current attempt.
	// It is intentionally runtime-only: the durable job owns the source of truth.
	ExecutionAttempt int
	// PausedFromStatus records the case status before a task-level pause, so
	// wake can continue the FSM from where it stopped.
	PausedFromStatus CaseStatus
	MaxDebateRounds  int
	Deadline         *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
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
	CaseStatusPaused           CaseStatus = "PAUSED"
)

type CasePhase string

// Constraint is a user-supplied decision constraint.
type Constraint struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Hard  bool   `json:"hard"`
}
