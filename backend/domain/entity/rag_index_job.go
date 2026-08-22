package entity

import "time"

// RagIndexJobKind is the operation a RAG index job performs.
type RagIndexJobKind string

const (
	RagIndexJobKindIndex  RagIndexJobKind = "index"
	RagIndexJobKindDelete RagIndexJobKind = "delete"
)

// RagIndexJobStatus mirrors the durable job state machine of DecisionJob.
type RagIndexJobStatus string

const (
	RagIndexJobQueued    RagIndexJobStatus = "queued"
	RagIndexJobRunning   RagIndexJobStatus = "running"
	RagIndexJobSucceeded RagIndexJobStatus = "succeeded"
	RagIndexJobFailed    RagIndexJobStatus = "failed"
	RagIndexJobCancelled RagIndexJobStatus = "cancelled"
)

// RagIndexJob is the durable execution envelope for one RAG index mutation.
// It mirrors DecisionJob so the same claim/lease/heartbeat/retry machinery
// can be reused against a different source table.
type RagIndexJob struct {
	ID          string
	Kind        RagIndexJobKind
	Source      string
	SourceRef   string
	Status      RagIndexJobStatus
	Attempt     int
	MaxAttempts int
	WorkerID    string
	LeaseUntil  *time.Time
	AvailableAt time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
