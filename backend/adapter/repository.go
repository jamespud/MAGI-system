package magi

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// --- helpers ---

func toJSON(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func fromJSON[T any](s string) T {
	var v T
	if s != "" {
		_ = json.Unmarshal([]byte(s), &v)
	}
	return v
}

// --- aggregate ---

type magiRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) port.Repository {
	return &magiRepository{db: db}
}

// CleanupCaseArtifacts removes the persisted artifacts of a previous execution
// attempt so a resumed retry does not leave duplicate evidence/claims/votes.
// Checkpoints, events, approval history and resolutions are preserved.
func (r *magiRepository) CleanupCaseArtifacts(ctx context.Context, caseID string) error {
	if r.db == nil {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("case_id = ?", caseID).Delete(&ToolCallModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("case_id = ?", caseID).Delete(&VoteModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("case_id = ?", caseID).Delete(&ClaimModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("case_id = ?", caseID).Delete(&EvidenceModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("case_id = ?", caseID).Delete(&DebateRoundModel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("agent_run_id IN (SELECT id FROM magi_agent_run WHERE case_id = ?)", caseID).Delete(&ReflectionModel{}).Error; err != nil {
			return err
		}
		return tx.Where("case_id = ?", caseID).Delete(&AgentRunModel{}).Error
	})
}

func (r *magiRepository) CaseRepo() port.CaseRepository             { return &caseRepo{db: r.db} }
func (r *magiRepository) AgentRunRepo() port.AgentRunRepository     { return &agentRunRepo{db: r.db} }
func (r *magiRepository) EvidenceRepo() port.EvidenceRepository     { return &evidenceRepo{db: r.db} }
func (r *magiRepository) ClaimRepo() port.ClaimRepository           { return &claimRepo{db: r.db} }
func (r *magiRepository) VoteRepo() port.VoteRepository             { return &voteRepo{db: r.db} }
func (r *magiRepository) DebateRepo() port.DebateRepository         { return &debateRepo{db: r.db} }
func (r *magiRepository) ReflectionRepo() port.ReflectionRepository { return &reflectionRepo{db: r.db} }
func (r *magiRepository) ResolutionRepo() port.ResolutionRepository { return &resolutionRepo{db: r.db} }
func (r *magiRepository) EventRepo() port.EventRepository           { return &eventRepo{db: r.db} }
func (r *magiRepository) CheckpointRepo() port.CheckpointRepository { return &checkpointRepo{db: r.db} }
func (r *magiRepository) MemoryRepo() port.MemoryRepository         { return &memoryRepo{db: r.db} }
func (r *magiRepository) ToolCallRepo() port.ToolCallRepository     { return &toolCallRepo{db: r.db} }

var _ port.Repository = (*magiRepository)(nil)

// --- CaseRepository ---

type caseRepo struct{ db *gorm.DB }

func (r *caseRepo) Create(ctx context.Context, c *entity.DecisionCase) error {
	m := caseToModel(c)
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *caseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	var m CaseModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return caseFromModel(&m), nil
}
func (r *caseRepo) UpdateStatus(ctx context.Context, id string, status entity.CaseStatus) error {
	return r.db.WithContext(ctx).Model(&CaseModel{}).Where("id = ?", id).Update("status", string(status)).Error
}
func (r *caseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return r.db.WithContext(ctx).Model(&CaseModel{}).Where("id = ?", id).Update("task_json", toJSON(task)).Error
}
func (r *caseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	var models []CaseModel
	if err := r.db.WithContext(ctx).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	cases := make([]*entity.DecisionCase, len(models))
	for i, m := range models {
		cases[i] = caseFromModel(&m)
	}
	return cases, nil
}

func caseToModel(c *entity.DecisionCase) CaseModel {
	return CaseModel{
		ID: c.ID, UserID: c.UserID, Question: c.Question, Context: c.Context,
		ConstraintsJSON: toJSON(c.Constraints), ParentCaseID: c.ParentCaseID, Status: string(c.Status),
		CurrentPhase: string(c.CurrentPhase),
		MaxDebateRounds: c.MaxDebateRounds, Deadline: c.Deadline, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}
func caseFromModel(m *CaseModel) *entity.DecisionCase {
	return &entity.DecisionCase{
		ID: m.ID, UserID: m.UserID, Question: m.Question, Context: m.Context,
		Constraints: fromJSON[[]entity.Constraint](m.ConstraintsJSON), ParentCaseID: m.ParentCaseID,
		Status: entity.CaseStatus(m.Status),
		CurrentPhase: entity.CasePhase(m.CurrentPhase), MaxDebateRounds: m.MaxDebateRounds, Deadline: m.Deadline,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// --- AgentRunRepository ---

type agentRunRepo struct{ db *gorm.DB }

func (r *agentRunRepo) Create(ctx context.Context, a *entity.AgentRun) error {
	m := AgentRunModel{
		ID: a.ID, CaseID: a.CaseID, MagiConfigID: a.MagiConfigID, MagiCode: string(a.MagiCode),
		Round: a.Round, Status: string(a.Status), UsageJSON: toJSON(a.Usage),
		EnvironmentJSON: toJSON(a.Environment), Err: a.Err,
		StartedAt: a.StartedAt, CompletedAt: a.CompletedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *agentRunRepo) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	var m AgentRunModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entity.AgentRun{
		ID: m.ID, CaseID: m.CaseID, MagiConfigID: m.MagiConfigID, MagiCode: entity.MagiCode(m.MagiCode),
		Round: m.Round, Status: entity.AgentRunStatus(m.Status), Usage: fromJSON[*entity.Usage](m.UsageJSON),
		Environment: fromJSON[*entity.RunEnvironment](m.EnvironmentJSON),
		Err: m.Err, StartedAt: m.StartedAt, CompletedAt: m.CompletedAt,
	}, nil
}
func (r *agentRunRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	var models []AgentRunModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.AgentRun, len(models))
	for i, m := range models {
		out[i] = &entity.AgentRun{
			ID: m.ID, CaseID: m.CaseID, MagiConfigID: m.MagiConfigID, MagiCode: entity.MagiCode(m.MagiCode),
			Round: m.Round, Status: entity.AgentRunStatus(m.Status), Usage: fromJSON[*entity.Usage](m.UsageJSON),
			Environment: fromJSON[*entity.RunEnvironment](m.EnvironmentJSON),
			Err: m.Err, StartedAt: m.StartedAt, CompletedAt: m.CompletedAt,
		}
	}
	return out, nil
}

// --- EvidenceRepository ---

type evidenceRepo struct{ db *gorm.DB }

func (r *evidenceRepo) Create(ctx context.Context, e *entity.EvidenceRecord) error {
	uri := ""
	if e.SourceURI != nil {
		uri = *e.SourceURI
	}
	m := EvidenceModel{
		ID: e.ID, CaseID: e.CaseID, AgentRunID: e.AgentRunID, ToolCallID: e.ToolCallID, ToolName: e.ToolName,
		SourceType: string(e.SourceType), SourceURI: uri, RawContent: e.RawContent, Observation: e.Observation,
		ReliabilityJSON: toJSON(e.Reliability), CollectedBy: string(e.CollectedBy), CreatedAt: e.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *evidenceRepo) Get(ctx context.Context, id string) (*entity.EvidenceRecord, error) {
	var m EvidenceModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return evidenceFromModel(&m), nil
}
func (r *evidenceRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error) {
	var models []EvidenceModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.EvidenceRecord, len(models))
	for i := range models {
		out[i] = evidenceFromModel(&models[i])
	}
	return out, nil
}

func evidenceFromModel(m *EvidenceModel) *entity.EvidenceRecord {
	uri := m.SourceURI
	return &entity.EvidenceRecord{
		ID: m.ID, CaseID: m.CaseID, AgentRunID: m.AgentRunID, ToolCallID: m.ToolCallID, ToolName: m.ToolName,
		SourceType: entity.EvidenceSourceType(m.SourceType), SourceURI: &uri, RawContent: m.RawContent,
		Observation: m.Observation, Reliability: fromJSON[entity.ReliabilityScore](m.ReliabilityJSON),
		CollectedBy: entity.MagiCode(m.CollectedBy), CreatedAt: m.CreatedAt,
	}
}

// --- ClaimRepository ---

type claimRepo struct{ db *gorm.DB }

func (r *claimRepo) Create(ctx context.Context, c *entity.Claim) error {
	m := ClaimModel{
		ID: c.ID, CaseID: c.CaseID, AgentRunID: c.AgentRunID, Statement: c.Statement,
		SupportsJSON: toJSON(c.Supports), ContradictsJSON: toJSON(c.Contradicts), Status: string(c.Status),
		CreatedBy: string(c.CreatedBy), CreatedAt: c.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *claimRepo) Get(ctx context.Context, id string) (*entity.Claim, error) {
	var m ClaimModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return claimFromModel(&m), nil
}
func (r *claimRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Claim, error) {
	var models []ClaimModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.Claim, len(models))
	for i := range models {
		out[i] = claimFromModel(&models[i])
	}
	return out, nil
}

func claimFromModel(m *ClaimModel) *entity.Claim {
	return &entity.Claim{
		ID: m.ID, CaseID: m.CaseID, AgentRunID: m.AgentRunID, Statement: m.Statement,
		Supports: fromJSON[[]string](m.SupportsJSON), Contradicts: fromJSON[[]string](m.ContradictsJSON),
		Status: entity.ClaimStatus(m.Status), CreatedBy: entity.MagiCode(m.CreatedBy), CreatedAt: m.CreatedAt,
	}
}

// --- VoteRepository ---

type voteRepo struct{ db *gorm.DB }

func (r *voteRepo) Create(ctx context.Context, v *entity.Vote) error {
	m := VoteModel{
		ID: v.ID, CaseID: v.CaseID, AgentRunID: v.AgentRunID, Round: v.Round, Decision: string(v.Decision),
		Confidence: v.Confidence, UtilityScoresJSON: toJSON(v.UtilityScores), KeyClaimIDsJSON: toJSON(v.KeyClaimIDs),
		EvidenceIDsJSON: toJSON(v.EvidenceIDs), ReasoningSummary: v.ReasoningSummary, ConditionsJSON: toJSON(v.Conditions),
		CreatedAt: v.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *voteRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Vote, error) {
	var models []VoteModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.Vote, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.Vote{
			ID: m.ID, CaseID: m.CaseID, AgentRunID: m.AgentRunID, Round: m.Round, Decision: entity.VoteDecision(m.Decision),
			Confidence: m.Confidence, UtilityScores: fromJSON[[]entity.UtilityDimensionScore](m.UtilityScoresJSON),
			KeyClaimIDs: fromJSON[[]string](m.KeyClaimIDsJSON), EvidenceIDs: fromJSON[[]string](m.EvidenceIDsJSON),
			ReasoningSummary: m.ReasoningSummary, Conditions: fromJSON[[]entity.DecisionCondition](m.ConditionsJSON),
			CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

// --- ResolutionRepository ---

type resolutionRepo struct{ db *gorm.DB }

func (r *resolutionRepo) Create(ctx context.Context, res *entity.Resolution) error {
	m := ResolutionModel{
		ID: res.ID, CaseID: res.CaseID, ConsensusJSON: toJSON(res.Consensus), FinalDecision: string(res.FinalDecision),
		FinalReport: res.FinalReport, KeyEvidenceIDsJSON: toJSON(res.KeyEvidenceIDs), KeyClaimIDsJSON: toJSON(res.KeyClaimIDs),
		VoteIDsJSON: toJSON(res.VoteIDs), EvaluationJSON: toJSON(res.Evaluation), CreatedAt: res.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *resolutionRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	var m ResolutionModel
	if err := r.db.WithContext(ctx).First(&m, "case_id = ?", caseID).Error; err != nil {
		return nil, err
	}
	return &entity.Resolution{
		ID: m.ID, CaseID: m.CaseID, Consensus: fromJSON[entity.ConsensusResult](m.ConsensusJSON),
		FinalDecision: entity.VoteDecision(m.FinalDecision), FinalReport: m.FinalReport,
		KeyEvidenceIDs: fromJSON[[]string](m.KeyEvidenceIDsJSON), KeyClaimIDs: fromJSON[[]string](m.KeyClaimIDsJSON),
		VoteIDs: fromJSON[[]string](m.VoteIDsJSON), Evaluation: fromJSON[*entity.Evaluation](m.EvaluationJSON), CreatedAt: m.CreatedAt,
	}, nil
}

// --- EventRepository ---

type eventRepo struct{ db *gorm.DB }

func (r *eventRepo) Create(ctx context.Context, e *entity.MagiEvent) error {
	m := EventModel{
		ID: e.ID, CaseID: e.CaseID, RunID: e.RunID, AgentCode: "", Type: string(e.Type),
		PayloadJSON: string(e.Payload), Timestamp: e.Timestamp,
	}
	if e.AgentCode != nil {
		m.AgentCode = string(*e.AgentCode)
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *eventRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.MagiEvent, error) {
	var models []EventModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.MagiEvent, len(models))
	for i := range models {
		m := &models[i]
		var code *entity.MagiCode
		if m.AgentCode != "" {
			c := entity.MagiCode(m.AgentCode)
			code = &c
		}
		out[i] = &entity.MagiEvent{
			ID: m.ID, CaseID: m.CaseID, RunID: m.RunID, AgentCode: code, Type: entity.EventType(m.Type),
			Payload: json.RawMessage(m.PayloadJSON), Timestamp: m.Timestamp,
		}
	}
	return out, nil
}

// --- DebateRepository (DB) ---

type debateRepo struct{ db *gorm.DB }

func (r *debateRepo) Create(ctx context.Context, d *entity.DebateRound) error {
	m := DebateRoundModel{
		ID: d.ID, CaseID: d.CaseID, Round: d.Round, PacketJSON: toJSON(d.Packet),
		StartedAt: d.StartedAt, CompletedAt: d.CompletedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *debateRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.DebateRound, error) {
	var models []DebateRoundModel
	if err := r.db.WithContext(ctx).Where("case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.DebateRound, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.DebateRound{ID: m.ID, CaseID: m.CaseID, Round: m.Round,
			Packet: fromJSON[entity.DebatePacket](m.PacketJSON), StartedAt: m.StartedAt, CompletedAt: m.CompletedAt}
	}
	return out, nil
}

// --- ReflectionRepository (DB) ---

type reflectionRepo struct{ db *gorm.DB }

func (r *reflectionRepo) Create(ctx context.Context, rf *entity.Reflection) error {
	m := ReflectionModel{
		ID: rf.ID, AgentRunID: rf.AgentRunID, Round: rf.Round, PreviousVoteID: rf.PreviousVoteID,
		PositionChange: string(rf.PositionChange), AcceptedClaimsJSON: toJSON(rf.AcceptedClaims),
		RejectedClaimsJSON: toJSON(rf.RejectedClaims), NewEvidenceIDsJSON: toJSON(rf.NewEvidenceIDs),
		Reasoning: rf.Reasoning, ReadyToRevote: rf.ReadyToRevote, CreatedAt: rf.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}
func (r *reflectionRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Reflection, error) {
	var models []ReflectionModel
	if err := r.db.WithContext(ctx).Joins("JOIN magi_agent_run ON magi_agent_run.id = reflection.agent_run_id").Where("magi_agent_run.case_id = ?", caseID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.Reflection, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.Reflection{ID: m.ID, AgentRunID: m.AgentRunID, Round: m.Round,
			PreviousVoteID: m.PreviousVoteID, PositionChange: entity.PositionChange(m.PositionChange),
			AcceptedClaims: fromJSON[[]string](m.AcceptedClaimsJSON), RejectedClaims: fromJSON[[]string](m.RejectedClaimsJSON),
			NewEvidenceIDs: fromJSON[[]string](m.NewEvidenceIDsJSON), Reasoning: m.Reasoning, ReadyToRevote: m.ReadyToRevote, CreatedAt: m.CreatedAt}
	}
	return out, nil
}

// --- CheckpointRepository ---

type checkpointRepo struct{ db *gorm.DB }

func (r *checkpointRepo) Save(ctx context.Context, state *entity.AgentState) error {
	if state == nil || state.RunID == "" {
		return nil
	}
	m := CheckpointModel{
		RunID:           state.RunID,
		MessagesJSON:    state.MessagesJSON,
		MessagesRefJSON: toJSON(state.Messages),
		StepCount:       state.StepCount,
		TokenUsed:       state.TokenUsed,
		Phase:           state.Phase,
	}
	// Save is an upsert so every loop step has one durable snapshot per run.
	return r.db.WithContext(ctx).Save(&m).Error
}

func (r *checkpointRepo) Load(ctx context.Context, runID string) (*entity.AgentState, error) {
	if runID == "" {
		return nil, nil
	}
	var m CheckpointModel
	if err := r.db.WithContext(ctx).First(&m, "run_id = ?", runID).Error; err != nil {
		return nil, err
	}
	return &entity.AgentState{
		RunID:        m.RunID,
		Messages:     fromJSON[[]entity.MessageRef](m.MessagesRefJSON),
		MessagesJSON: m.MessagesJSON,
		StepCount:    m.StepCount,
		TokenUsed:    m.TokenUsed,
		Phase:        m.Phase,
	}, nil
}

// --- MemoryRepository (DB) ---

type memoryRepo struct{ db *gorm.DB }

func (r *memoryRepo) Get(ctx context.Context, caseID string) (*entity.CaseMemoryProjection, error) {
	var m MemoryProjectionModel
	if err := r.db.WithContext(ctx).First(&m, "case_id = ?", caseID).Error; err != nil {
		return nil, err
	}
	return &entity.CaseMemoryProjection{
		CaseID: m.CaseID, QuestionSummary: m.QuestionSummary, ContextSummary: m.ContextSummary,
		KeyEvidence: fromJSON[[]entity.MemoryEvidence](m.KeyEvidenceJSON), KeyClaims: fromJSON[[]entity.MemoryClaim](m.KeyClaimsJSON),
		Votes: fromJSON[[]entity.MemoryVote](m.VotesJSON), Resolution: m.Resolution,
		Outcome: fromJSON[*entity.CaseOutcome](m.OutcomeJSON), ProjectionVersion: m.ProjectionVersion,
	}, nil
}
func (r *memoryRepo) Save(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	m := MemoryProjectionModel{
		CaseID: proj.CaseID, QuestionSummary: proj.QuestionSummary, ContextSummary: proj.ContextSummary,
		KeyEvidenceJSON: toJSON(proj.KeyEvidence), KeyClaimsJSON: toJSON(proj.KeyClaims),
		VotesJSON: toJSON(proj.Votes), Resolution: proj.Resolution, OutcomeJSON: toJSON(proj.Outcome),
		ProjectionVersion: proj.ProjectionVersion,
	}
	return r.db.WithContext(ctx).Save(&m).Error
}

// Search provides a deterministic local fallback for historical decision
// retrieval when a Coze Knowledge service is not configured. Coze remains the
// preferred semantic retriever; this fallback keeps Memory useful in the
// standalone/server deployment and is deliberately read-only.
func (r *memoryRepo) Search(ctx context.Context, query string, limit int) ([]*entity.CaseMemoryProjection, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	pattern := "%" + strings.ReplaceAll(query, "%", "\\%") + "%"
	var models []MemoryProjectionModel
	if err := r.db.WithContext(ctx).
		Where("question_summary LIKE ? OR context_summary LIKE ? OR resolution LIKE ?", pattern, pattern, pattern).
		Order("projection_version DESC").Limit(limit).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.CaseMemoryProjection, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.CaseMemoryProjection{
			CaseID: m.CaseID, QuestionSummary: m.QuestionSummary, ContextSummary: m.ContextSummary,
			KeyEvidence: fromJSON[[]entity.MemoryEvidence](m.KeyEvidenceJSON),
			KeyClaims:   fromJSON[[]entity.MemoryClaim](m.KeyClaimsJSON),
			Votes:       fromJSON[[]entity.MemoryVote](m.VotesJSON), Resolution: m.Resolution,
			Outcome: fromJSON[*entity.CaseOutcome](m.OutcomeJSON), ProjectionVersion: m.ProjectionVersion,
		}
	}
	return out, nil
}

// --- ToolCallRepository ---

type toolCallRepo struct{ db *gorm.DB }

func (r *toolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error {
	m := ToolCallModel{
		ID: t.ID, AgentRunID: t.AgentRunID, ToolCallID: t.ToolCallID, ToolName: t.ToolName,
		Arguments: t.Arguments, Valid: t.Valid, Result: t.Result, Err: t.Err, ApprovedBy: t.ApprovedBy,
		EvidenceID: t.EvidenceID, DurationMs: t.DurationMs, CreatedAt: t.CreatedAt,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

func (r *toolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	var models []ToolCallModel
	err := r.db.WithContext(ctx).
		Joins("JOIN magi_agent_run ON magi_agent_run.id = magi_tool_call.agent_run_id").
		Where("magi_agent_run.case_id = ?", caseID).
		Order("magi_tool_call.created_at ASC").
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	out := make([]*entity.ToolCall, len(models))
	for i, m := range models {
		out[i] = &entity.ToolCall{
			ID: m.ID, AgentRunID: m.AgentRunID, ToolCallID: m.ToolCallID, ToolName: m.ToolName,
			Arguments: m.Arguments, Valid: m.Valid, Result: m.Result, Err: m.Err, ApprovedBy: m.ApprovedBy,
			EvidenceID: m.EvidenceID, DurationMs: m.DurationMs, CreatedAt: m.CreatedAt,
		}
	}
	return out, nil
}

var _ port.CaseRepository = (*caseRepo)(nil)
var _ port.EventRepository = (*eventRepo)(nil)
var _ port.ToolCallRepository = (*toolCallRepo)(nil)
