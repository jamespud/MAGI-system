package magi

import "time"

// CaseModel is the GORM model for decision_case (ADR-007: lives in adapter layer).
type CaseModel struct {
	ID              string `gorm:"primaryKey"`
	UserID          int64
	Question        string `gorm:"type:text"`
	Context         string `gorm:"type:text"`
	ConstraintsJSON string `gorm:"type:text"`
	ParentCaseID    string `gorm:"index"`
	Status          string
	CurrentPhase    string
	MaxDebateRounds int
	Deadline        *time.Time
	TaskJSON        string `gorm:"type:text"`
	Pinned          bool
	Archived        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (CaseModel) TableName() string { return "decision_case" }

type AgentRunModel struct {
	ID              string `gorm:"primaryKey"`
	CaseID          string `gorm:"index"`
	MagiConfigID    string
	MagiCode        string
	Round           int
	Status          string
	UsageJSON       string `gorm:"type:text"`
	EnvironmentJSON string `gorm:"type:text"`
	Err             string `gorm:"type:text"`
	CheckpointJSON  string `gorm:"type:text"`
	SummaryJSON     string `gorm:"type:text"`
	StartedAt       time.Time
	CompletedAt     *time.Time
}

func (AgentRunModel) TableName() string { return "magi_agent_run" }

// DecisionJobModel is the durable worker envelope for one case.
type DecisionJobModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"uniqueIndex"`
	Status      string `gorm:"index"`
	Attempt     int
	MaxAttempts int
	WorkerID    string
	LeaseUntil  *time.Time
	AvailableAt time.Time `gorm:"index"`
	LastError   string    `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (DecisionJobModel) TableName() string { return "decision_job" }

// CheckpointModel stores the durable working-memory snapshot used by the
// runtime to resume an interrupted agent run. It intentionally lives in its
// own table: checkpoint writes are frequent and should not rewrite the
// relatively stable AgentRun row.
type CheckpointModel struct {
	RunID           string `gorm:"primaryKey"`
	MessagesJSON    string `gorm:"type:mediumtext"`
	MessagesRefJSON string `gorm:"type:text"`
	StepCount       int
	TokenUsed       int
	Phase           string
}

func (CheckpointModel) TableName() string { return "magi_agent_checkpoint" }

type EvidenceModel struct {
	ID              string `gorm:"primaryKey"`
	CaseID          string `gorm:"index"`
	AgentRunID      string `gorm:"index"`
	ToolCallID      string
	ToolName        string
	SourceType      string
	SourceURI       string `gorm:"type:varchar(512)"`
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
	EvaluationJSON     string `gorm:"type:text"`
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
	PacketJSON  string `gorm:"type:mediumtext"`
	StartedAt   time.Time
	CompletedAt *time.Time
}

func (DebateRoundModel) TableName() string { return "debate_round" }

type ReflectionModel struct {
	ID                 string `gorm:"primaryKey"`
	AgentRunID         string `gorm:"index"`
	Round              int
	PreviousVoteID     string
	PositionChange     string
	AcceptedClaimsJSON string `gorm:"type:text"`
	RejectedClaimsJSON string `gorm:"type:text"`
	NewEvidenceIDsJSON string `gorm:"type:text"`
	Reasoning          string `gorm:"type:text"`
	ReadyToRevote      bool
	CreatedAt          time.Time
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
	Annotation        string `gorm:"type:text"`
	TagsJSON          string `gorm:"type:text"`
	ProjectionVersion int
}

func (MemoryProjectionModel) TableName() string { return "case_memory_projection" }

type ToolCallModel struct {
	ID         string `gorm:"primaryKey"`
	AgentRunID string `gorm:"index"`
	ToolCallID string
	ToolName   string
	Arguments  string `gorm:"type:text"`
	Valid      bool
	Result     string `gorm:"type:text"`
	Err        string `gorm:"type:text"`
	ApprovedBy string
	EvidenceID string
	DurationMs int64
	CreatedAt  time.Time
}

func (ToolCallModel) TableName() string { return "magi_tool_call" }

type ApprovalModel struct {
	ID          string `gorm:"primaryKey"`
	CaseID      string `gorm:"index"`
	RunID       string
	AgentCode   string
	ToolName    string
	Arguments   string `gorm:"type:text"`
	Status      string `gorm:"index"`
	Reason      string `gorm:"type:text"`
	DecidedBy   string
	RequestedAt time.Time
	DecidedAt   *time.Time
	CreatedAt   time.Time
}

func (ApprovalModel) TableName() string { return "magi_approval_request" }

type DatasetModel struct {
	ID          string `gorm:"primaryKey"`
	OwnerID     int64
	Name        string
	Description string `gorm:"type:text"`
	ItemCount   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (DatasetModel) TableName() string { return "magi_dataset" }

type DatasetItemModel struct {
	ID               string `gorm:"primaryKey"`
	DatasetID        string `gorm:"index"`
	Question         string `gorm:"type:text"`
	Context          string `gorm:"type:text"`
	ConstraintsJSON  string `gorm:"type:text"`
	ExpectedDecision string
	Weight           float64
	TagsJSON         string `gorm:"type:text"`
	CreatedAt        time.Time
}

func (DatasetItemModel) TableName() string { return "magi_dataset_item" }

type BenchmarkRunModel struct {
	ID                  string `gorm:"primaryKey"`
	DatasetID           string `gorm:"index"`
	Status              string `gorm:"index"`
	LeaseOwner          string `gorm:"column:lease_owner"`
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

func (BenchmarkRunModel) TableName() string { return "magi_benchmark_run" }

type BenchmarkItemResultModel struct {
	ID               string `gorm:"primaryKey"`
	RunID            string `gorm:"index"`
	DatasetItemID    string
	CaseID           string
	ExpectedDecision string
	ActualDecision   string
	Matched          bool
	Score            float64
	Runs             int
	Consistency      float64
	DecisionsJSON    string `gorm:"type:text"`
	Error            string `gorm:"type:text"`
	Feedback         string `gorm:"type:text"`
	FeedbackAt       *time.Time
	CreatedAt        time.Time
}

func (BenchmarkItemResultModel) TableName() string { return "magi_benchmark_item_result" }

type PluginBindingModel struct {
	ID        string `gorm:"primaryKey"`
	UserID    int64  `gorm:"index"`
	PluginID  int64
	ToolID    int64
	IsDraft   bool
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (PluginBindingModel) TableName() string { return "magi_plugin_binding" }

type SchedulerLockModel struct {
	Name       string `gorm:"primaryKey"`
	Owner      string
	LeaseUntil time.Time
	UpdatedAt  time.Time
}

func (SchedulerLockModel) TableName() string { return "magi_scheduler_lock" }

type ToolQuotaCounterModel struct {
	UserID      int64     `gorm:"primaryKey"`
	ToolName    string    `gorm:"primaryKey"`
	WindowStart time.Time `gorm:"primaryKey"`
	Calls       int
}

func (ToolQuotaCounterModel) TableName() string { return "magi_tool_quota_counter" }

type JudgeModel struct {
	ID                  uint   `gorm:"primaryKey;autoIncrement"`
	CaseID              string `gorm:"uniqueIndex"`
	ReportQuality       float64
	EvidenceConsistency float64
	ReflectionValidity  float64
	Overall             float64
	Rationale           string `gorm:"type:text"`
	ModelName           string
	CreatedAt           time.Time
}

func (JudgeModel) TableName() string { return "magi_judge_eval" }

type RecurringCaseModel struct {
	ID              string `gorm:"primaryKey"`
	UserID          int64  `gorm:"index"`
	Name            string
	Question        string `gorm:"type:text"`
	Context         string `gorm:"type:text"`
	ConstraintsJSON string `gorm:"type:text"`
	IntervalMillis  int64
	Enabled         bool
	LastRunAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (RecurringCaseModel) TableName() string { return "magi_recurring_case" }

// AllModels returns all GORM models for AutoMigrate.
func AllModels() []any {
	return []any{
		&CaseModel{}, &AgentRunModel{}, &DecisionJobModel{}, &CheckpointModel{}, &EvidenceModel{}, &ClaimModel{},
		&VoteModel{}, &ResolutionModel{}, &EventModel{},
		&DebateRoundModel{}, &ReflectionModel{}, &MemoryProjectionModel{},
		&ToolCallModel{}, &ApprovalModel{}, &DatasetModel{}, &DatasetItemModel{}, &BenchmarkRunModel{}, &BenchmarkItemResultModel{},
		&PluginBindingModel{},
		&RecurringCaseModel{},
		&JudgeModel{},
		&SchedulerLockModel{},
		&ToolQuotaCounterModel{},
		&RunCounterModel{},
		&KnowledgeDocModel{},
		&UserModel{},
		&ApiKeyModel{},
		&PromptTemplateModel{},
		&ConversationModel{},
		&ConversationMessageModel{},
	}
}

// KnowledgeDocModel persists a user-uploaded knowledge document.
type KnowledgeDocModel struct {
	ID         string `gorm:"primaryKey;size:64"`
	UserID     int64  `gorm:"index"`
	Title      string
	Content    string `gorm:"type:longtext"`
	SourceKind string `gorm:"size:16"`
	SourceURL  string
	Status     string `gorm:"size:16"`
	Error      string
	Chunks     int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (KnowledgeDocModel) TableName() string { return "knowledge_docs" }

// UserModel persists a harness account.
type UserModel struct {
	ID        int64 `gorm:"primaryKey;autoIncrement"`
	Name      string
	Role      string `gorm:"size:16;default:user"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (UserModel) TableName() string { return "users" }

// ApiKeyModel persists a DB-backed API key (hash only).
type ApiKeyModel struct {
	ID         string `gorm:"primaryKey;size:64"`
	UserID     int64  `gorm:"index"`
	Name       string
	Prefix     string `gorm:"size:32"`
	KeyHash    string `gorm:"size:64;index"`
	LastUsedAt *time.Time
	Revoked    bool
	CreatedAt  time.Time
}

func (ApiKeyModel) TableName() string { return "api_keys" }

// ConversationModel persists a multi-turn assistant conversation thread.
type ConversationModel struct {
	ID        string `gorm:"primaryKey;size:64"`
	UserID    int64  `gorm:"index"`
	Title     string `gorm:"size:200"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ConversationModel) TableName() string { return "magi_conversation" }

// ConversationMessageModel persists one turn of a conversation.
type ConversationMessageModel struct {
	ID             string `gorm:"primaryKey;size:64"`
	ConversationID string `gorm:"index;size:64"`
	UserID         int64
	Role           string    `gorm:"size:16"`
	Content        string    `gorm:"type:longtext"`
	CaseID         string    `gorm:"size:64;index"`
	CreatedAt      time.Time `gorm:"index"`
}

func (ConversationMessageModel) TableName() string { return "magi_conversation_message" }
