package entity

import "time"

// DecisionJob is the durable execution envelope for one case run. It is
// intentionally separate from Case: a case describes the decision, while the
// job describes delivery, leasing, retry and recovery state.
type DecisionJob struct {
	ID          string
	CaseID      string
	Status      DecisionJobStatus
	Attempt     int
	MaxAttempts int
	WorkerID    string
	LeaseUntil  *time.Time
	AvailableAt time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type DecisionJobStatus string

const (
	DecisionJobQueued    DecisionJobStatus = "queued"
	DecisionJobRunning   DecisionJobStatus = "running"
	DecisionJobSucceeded DecisionJobStatus = "succeeded"
	DecisionJobFailed    DecisionJobStatus = "failed"
	DecisionJobCancelled DecisionJobStatus = "cancelled"
	DecisionJobPaused    DecisionJobStatus = "paused"
)
