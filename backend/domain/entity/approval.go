package entity

import "time"

// ApprovalStatus is the lifecycle of a human-in-the-loop tool approval request.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

// ApprovalRequest is a persisted human-approval request for a gated tool call.
// The agent loop parks the run while the request is pending and resumes
// execution when a human approves it, or feeds the rejection back to the model.
type ApprovalRequest struct {
	ID        string
	CaseID    string
	RunID     string
	AgentCode MagiCode
	ToolName  string
	Arguments string
	// IntentDigest binds the decision to one exact invocation intent (tool plus
	// canonical arguments). A later call to the same tool with different
	// arguments has a different digest and can never inherit this approval.
	IntentDigest string
	Status       ApprovalStatus
	Reason       string
	DecidedBy    string
	RequestedAt  time.Time
	DecidedAt    *time.Time
	CreatedAt    time.Time
}
