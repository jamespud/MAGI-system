package entity

import "time"

// Reflection is a Magi's reconsideration output after a debate round.
type Reflection struct {
	ID              string
	AgentRunID      string
	Round           int
	PreviousVoteID  string
	PositionChange  PositionChange
	AcceptedClaims  []string // Claim-IDs
	RejectedClaims  []string // Claim-IDs
	NewEvidenceIDs  []string // EV-IDs
	Reasoning       string
	ReadyToRevote   bool
	CreatedAt       time.Time
}

type PositionChange string

const (
	PositionChangeMaintain  PositionChange = "maintain"
	PositionChangeStrengthen PositionChange = "strengthen"
	PositionChangeWeaken    PositionChange = "weaken"
	PositionChangeChange    PositionChange = "change"
	PositionChangeAbstain   PositionChange = "abstain"
)
