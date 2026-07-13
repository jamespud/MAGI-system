package entity

import "time"

// Claim is an agent's interpretation about the world, grounded in evidence
// (ADR-005). Supports reference EV-IDs; Contradicts reference other Claim-IDs.
type Claim struct {
	ID          string
	CaseID      string
	AgentRunID  string
	Statement   string
	Supports    []string // EV-IDs
	Contradicts []string // Claim-IDs
	Status      ClaimStatus
	CreatedBy   MagiCode
	CreatedAt   time.Time
}

type ClaimStatus string

const (
	ClaimStatusOpen       ClaimStatus = "open"
	ClaimStatusSupported  ClaimStatus = "supported"
	ClaimStatusRefuted    ClaimStatus = "refuted"
	ClaimStatusSuperseded ClaimStatus = "superseded"
)
