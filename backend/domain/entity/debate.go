package entity

import "time"

// DebateRound is one round of structured debate over conflicting claims.
type DebateRound struct {
	ID          string
	CaseID      string
	Round       int
	Packet      DebatePacket
	StartedAt   time.Time
	CompletedAt *time.Time
}

// DebatePacket is sent to all three Magi (minority AND majority).
type DebatePacket struct {
	Round             int
	MajorityVotes     []Vote
	MinorityVotes     []Vote
	ConflictingClaims []ClaimConflict
	SharedEvidence    []EvidenceRecord
	Questions         []DebateQuestion
}

type ClaimConflict struct {
	ClaimA string
	ClaimB string
	Reason string
}

type DebateQuestion struct {
	From MagiCode
	To   MagiCode
	Text string
}
