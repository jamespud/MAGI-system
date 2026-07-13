package magi

import "time"

// CaseModel is the GORM model for decision_case (ADR-007: lives in adapter layer).
type CaseModel struct {
	ID              string `gorm:"primaryKey"`
	UserID          int64
	Question        string `gorm:"type:text"`
	Context         string `gorm:"type:text"`
	ConstraintsJSON string `gorm:"type:text"`
	Status          string
	CurrentPhase    string
	MaxDebateRounds int
	Deadline        *time.Time
	TaskJSON        string `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (CaseModel) TableName() string { return "decision_case" }

type AgentRunModel struct {
	ID             string `gorm:"primaryKey"`
	CaseID         string `gorm:"index"`
	MagiConfigID   string
	MagiCode       string
	Round          int
	Status         string
	UsageJSON      string `gorm:"type:text"`
	Err            string `gorm:"type:text"`
	CheckpointJSON string `gorm:"type:text"`
	SummaryJSON    string `gorm:"type:text"`
	StartedAt      time.Time
	CompletedAt    *time.Time
}

func (AgentRunModel) TableName() string { return "magi_agent_run" }

type EvidenceModel struct {
	ID              string `gorm:"primaryKey"`
	CaseID          string `gorm:"index"`
	AgentRunID      string `gorm:"index"`
	ToolCallID      string
	ToolName        string
	SourceType      string
	SourceURI       string
	RawContent      string `gorm:"type:text"`
	Observation     string `gorm:"type:text"`
	ReliabilityJSON string `gorm:"type:text"`
	CollectedBy     string
	CreatedAt       time.Time
}

func (EvidenceModel) TableName() string { return "evidence_record" }

type ClaimModel struct {
	ID              string `gorm:"primaryKey"`
	CaseID          string `gorm:"index"`
	AgentRunID      string `gorm:"index"`
	Statement       string `gorm:"type:text"`
	SupportsJSON    string `gorm:"type:text"`
	ContradictsJSON string `gorm:"type:text"`
	Status          string
	CreatedBy       string
	CreatedAt       time.Time
}

func (ClaimModel) TableName() string { return "claim" }

type VoteModel struct {
	ID                string `gorm:"primaryKey"`
	CaseID            string `gorm:"index"`
	AgentRunID        string
	Round             int
	Decision          string
	Confidence        float64
	UtilityScoresJSON string `gorm:"type:text"`
	KeyClaimIDsJSON   string `gorm:"type:text"`
	EvidenceIDsJSON   string `gorm:"type:text"`
	ReasoningSummary  string `gorm:"type:text"`
	ConditionsJSON    string `gorm:"type:text"`
	CreatedAt         time.Time
}

func (VoteModel) TableName() string { return "magi_vote" }

type ResolutionModel struct {
	ID                 string `gorm:"primaryKey"`
	CaseID             string `gorm:"uniqueIndex"`
	ConsensusJSON      string `gorm:"type:text"`
	FinalDecision      string
	FinalReport        string `gorm:"type:text"`
	KeyEvidenceIDsJSON string `gorm:"type:text"`
	KeyClaimIDsJSON    string `gorm:"type:text"`
	VoteIDsJSON        string `gorm:"type:text"`
	CreatedAt          time.Time
}

func (ResolutionModel) TableName() string { return "resolution" }

type EventModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"index"`
	RunID       string
	AgentCode   string
	Type        string
	PayloadJSON string `gorm:"type:text"`
	Timestamp   time.Time
}

func (EventModel) TableName() string { return "magi_event" }

type DebateRoundModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"index"`
	Round       int
	PacketJSON  string `gorm:"type:text"`
	StartedAt   time.Time
	CompletedAt *time.Time
}

func (DebateRoundModel) TableName() string { return "debate_round" }

type ReflectionModel struct {
	ID                  string `gorm:"primaryKey"`
	AgentRunID          string `gorm:"index"`
	Round               int
	PreviousVoteID      string
	PositionChange      string
	AcceptedClaimsJSON  string `gorm:"type:text"`
	RejectedClaimsJSON  string `gorm:"type:text"`
	NewEvidenceIDsJSON  string `gorm:"type:text"`
	Reasoning           string `gorm:"type:text"`
	ReadyToRevote       bool
	CreatedAt           time.Time
}

func (ReflectionModel) TableName() string { return "reflection" }

type MemoryProjectionModel struct {
	CaseID            string `gorm:"primaryKey"`
	QuestionSummary   string `gorm:"type:text"`
	ContextSummary    string `gorm:"type:text"`
	KeyEvidenceJSON   string `gorm:"type:text"`
	KeyClaimsJSON     string `gorm:"type:text"`
	VotesJSON         string `gorm:"type:text"`
	Resolution        string `gorm:"type:text"`
	OutcomeJSON       string `gorm:"type:text"`
	ProjectionVersion int
}

func (MemoryProjectionModel) TableName() string { return "case_memory_projection" }

// AllModels returns all GORM models for AutoMigrate.
func AllModels() []any {
	return []any{
		&CaseModel{}, &AgentRunModel{}, &EvidenceModel{}, &ClaimModel{},
		&VoteModel{}, &ResolutionModel{}, &EventModel{},
		&DebateRoundModel{}, &ReflectionModel{}, &MemoryProjectionModel{},
	}
}
