package entity

import "time"

// GoldenCase is a real production decision promoted to the online-golden
// regression set. Its expected decision comes from the completed case.
type GoldenCase struct {
	ID               string
	CaseID           string
	Question         string
	Context          string
	ExpectedDecision VoteDecision
	CreatedAt        time.Time
}
