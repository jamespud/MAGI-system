package entity

import "time"

// BenchmarkDataset is a named collection of ground-truth decision cases.
type BenchmarkDataset struct {
	ID          string
	OwnerID     int64
	Name        string
	Description string
	ItemCount   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BenchmarkItem is one ground-truth case with an expected decision.
type BenchmarkItem struct {
	ID               string
	DatasetID        string
	Question         string
	Context          string
	Constraints      []Constraint
	ExpectedDecision VoteDecision
	Weight           float64
	Tags             []string
	CreatedAt        time.Time
}

// BenchmarkRun is one execution of a dataset against the orchestrator.
type BenchmarkRun struct {
	ID                  string
	DatasetID           string
	Status              BenchmarkRunStatus
	LeaseOwner          string
	LeaseUntil          *time.Time
	Total               int
	Matched             int
	Accuracy            float64
	WeightedAccuracy    float64
	RunsPerItem         int
	Stability           float64
	RegressionThreshold float64
	RegressionFailed    bool
	FailureReason       string
	StartedAt           time.Time
	CompletedAt         *time.Time
	CreatedAt           time.Time
}

type BenchmarkRunStatus string

const (
	BenchmarkRunQueued    BenchmarkRunStatus = "queued"
	BenchmarkRunRunning   BenchmarkRunStatus = "running"
	BenchmarkRunSucceeded BenchmarkRunStatus = "succeeded"
	BenchmarkRunFailed    BenchmarkRunStatus = "failed"
)

// BenchmarkItemResult is the per-case outcome of a benchmark run.
type BenchmarkItemResult struct {
	ID               string
	RunID            string
	DatasetItemID    string
	CaseID           string
	ExpectedDecision VoteDecision
	ActualDecision   VoteDecision
	Matched          bool
	Score            float64
	Runs             int
	Consistency      float64
	Decisions        []VoteDecision
	Error            string
	Feedback         string
	FeedbackAt       *time.Time
	CreatedAt        time.Time
}
