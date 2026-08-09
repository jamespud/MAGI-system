package entity

import "time"

// JudgeResult is the semantic evaluation produced by LLM-as-a-Judge for a
// completed case. Scores are 0-100.
type JudgeResult struct {
	CaseID              string
	ReportQuality       float64
	EvidenceConsistency float64
	ReflectionValidity  float64
	Overall             float64
	Rationale           string
	ModelName           string
	CreatedAt           time.Time
}
