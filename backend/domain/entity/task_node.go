package entity

import "time"

// TaskNode is one node of the persisted task state tree for a case: an agent
// execution, a delegated sub-investigation, or a phase marker.
type TaskNode struct {
	ID          string
	CaseID      string
	ParentID    string
	RunID       string
	Kind        string // agent | delegate | phase
	Title       string
	Status      string
	Detail      string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

const (
	TaskNodeKindAgent    = "agent"
	TaskNodeKindDelegate = "delegate"
	TaskNodeKindPhase    = "phase"
)
