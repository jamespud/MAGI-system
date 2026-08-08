package dto

import (
	"encoding/json"
	"time"

	"github.com/jamespud/magi/backend/application/admin"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type CreateCaseRequest struct {
	Question    string          `json:"question"`
	Background  string          `json:"background,omitempty"`
	Constraints []ConstraintDTO `json:"constraints,omitempty"`
}

type CaseResponse struct {
	ID            string          `json:"id"`
	Question      string          `json:"question"`
	Background    string          `json:"background"`
	Constraints   []ConstraintDTO `json:"constraints"`
	Status        string          `json:"status"`
	Consensus     *ConsensusDTO   `json:"consensus,omitempty"`
	FinalDecision string          `json:"final_decision,omitempty"`
	Confidence    float64         `json:"confidence"`
	Round         int             `json:"round"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type ConstraintDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type ConsensusDTO struct {
	Approve        int    `json:"approve"`
	Reject         int    `json:"reject"`
	Abstain        int    `json:"abstain"`
	Majority       string `json:"majority"`
	NeedReflection bool   `json:"need_reflection"`
}

type CaseListResponse struct {
	Cases []CaseResponse `json:"cases"`
}

type AgentSnapshotDTO struct {
	AgentCode string        `json:"agent_code"`
	Status    string        `json:"status"`
	Round     int           `json:"round"`
	Step      int           `json:"step"`
	ToolCalls []ToolCallDTO `json:"tool_calls"`
	Evidence  []EvidenceDTO `json:"evidence"`
	Claims    []ClaimDTO    `json:"claims"`
	Vote      *VoteDTO      `json:"vote,omitempty"`
}

type ToolCallDTO struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
	Result     string `json:"result"`
	Err        string `json:"err,omitempty"`
	EvidenceID string `json:"evidence_id,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

type EvidenceDTO struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	URL         string  `json:"url,omitempty"`
	Observation string  `json:"observation"`
	Reliability float64 `json:"reliability"`
	CollectedBy string  `json:"collected_by"`
	Timestamp   string  `json:"timestamp"`
}

type ClaimDTO struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Supports    []string `json:"supports"`
	Contradicts []string `json:"contradicts"`
	CreatedBy   string   `json:"created_by"`
}

type VoteDTO struct {
	ID         string  `json:"id"`
	AgentCode  string  `json:"agent_code"`
	Stance     string  `json:"stance"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
	Round      int     `json:"round"`
}

type DecisionReport struct {
	Report string `json:"report"`
}

type ReplayEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	AgentCode string          `json:"agent_code,omitempty"`
	RunID     string          `json:"run_id,omitempty"`
	Message   string          `json:"message"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

type EvaluationResponse struct {
	ToolSuccessRate     float64 `json:"tool_success_rate"`
	GateFailures        int     `json:"gate_failures"`
	TotalTokens         int64   `json:"total_tokens"`
	FirstRoundConsensus bool    `json:"first_round_consensus"`
	ConsensusRound      int     `json:"consensus_round"`
}

type ToolResponse struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func FromCase(c *entity.DecisionCase, resolution *entity.Resolution) CaseResponse {
	constraints := make([]ConstraintDTO, len(c.Constraints))
	for i, ct := range c.Constraints {
		constraints[i] = ConstraintDTO{Label: ct.Key, Value: ct.Value}
	}
	resp := CaseResponse{
		ID:          c.ID,
		Question:    c.Question,
		Background:  c.Context,
		Constraints: constraints,
		Status:      string(c.Status),
		Round:       0,
		CreatedAt:   c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.Format(time.RFC3339),
	}
	if resolution != nil {
		resp.Round = resolution.Consensus.Round
		resp.FinalDecision = string(resolution.FinalDecision)
		resp.Consensus = consensusDTOFrom(resolution.Consensus)
		resp.Confidence = avgConfidence(resolution.Consensus.Votes)
	}
	return resp
}

func consensusDTOFrom(cr entity.ConsensusResult) *ConsensusDTO {
	var app, rej, abs int
	for _, v := range cr.Votes {
		switch v.Decision {
		case entity.VoteDecisionApprove, entity.VoteDecisionConditionalApprove:
			app++
		case entity.VoteDecisionReject:
			rej++
		default:
			abs++
		}
	}
	majority := "Tie"
	switch {
	case app > rej && app > abs:
		majority = "Approve"
	case rej > app && rej > abs:
		majority = "Reject"
	}
	return &ConsensusDTO{
		Approve:        app,
		Reject:         rej,
		Abstain:        abs,
		Majority:       majority,
		NeedReflection: cr.Outcome == entity.ConsensusConditional,
	}
}

func avgConfidence(votes []entity.Vote) float64 {
	if len(votes) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range votes {
		sum += entity.NormalizeConfidence(v.Confidence)
	}
	return sum / float64(len(votes))
}

func FromEvent(e *entity.MagiEvent) ReplayEvent {
	code := ""
	if e.AgentCode != nil {
		code = string(*e.AgentCode)
	}
	return ReplayEvent{
		ID:        e.ID,
		Type:      string(e.Type),
		AgentCode: code,
		RunID:     e.RunID,
		Message:   eventMessage(e),
		Payload:   e.Payload,
		Timestamp: e.Timestamp,
	}
}

func eventMessage(e *entity.MagiEvent) string {
	switch e.Type {
	case entity.EventCaseCreated:
		return "Case created"
	case entity.EventTaskNormalized:
		return "Task normalized"
	case entity.EventMemoryRetrieved:
		return "Memory retrieved"
	case entity.EventAgentStarted:
		return "Agents dispatched"
	case entity.EventModelRequested:
		return "Model request started"
	case entity.EventModelResponded:
		return "Model responded"
	case entity.EventToolCallRequested:
		return "Tool call requested"
	case entity.EventToolCallValidated:
		return "Tool call validated"
	case entity.EventToolCallStarted:
		return "Tool call started"
	case entity.EventToolCallCompleted:
		return "Tool call completed"
	case entity.EventToolCallFailed:
		return "Tool call failed"
	case entity.EventEvidenceCreated:
		return "Evidence created"
	case entity.EventClaimCreated:
		return "Claim created"
	case entity.EventEvidenceGatePassed:
		return "Evidence gate passed"
	case entity.EventEvidenceGateFailed:
		return "Evidence gate failed"
	case entity.EventVoteSubmitted:
		return "Votes submitted"
	case entity.EventConsensusEvaluated:
		return "Consensus evaluated"
	case entity.EventDebateStarted:
		return "Debate started"
	case entity.EventReflectionSubmitted:
		return "Reflection submitted"
	case entity.EventRevoteSubmitted:
		return "Revote submitted"
	case entity.EventResolutionCreated:
		return "Resolution created"
	case entity.EventMemoryIndexed:
		return "Memory indexed"
	case entity.EventCaseCompleted:
		return "Case completed"
	case entity.EventCaseFailed:
		return "Case failed"
	default:
		return string(e.Type)
	}
}

func FromTool(t port.ToolDefinition) ToolResponse {
	return ToolResponse{Name: t.Name, Desc: t.Desc}
}

func FromEvaluation(e *entity.Evaluation) EvaluationResponse {
	return EvaluationResponse{
		ToolSuccessRate:     e.ToolSuccessRate,
		GateFailures:        e.GateFailures,
		TotalTokens:         e.TotalTokens,
		FirstRoundConsensus: e.FirstRoundConsensus,
		ConsensusRound:      e.ConsensusRound,
	}
}

func FromEvidence(e *entity.EvidenceRecord) EvidenceDTO {
	url := ""
	if e.SourceURI != nil {
		url = *e.SourceURI
	}
	return EvidenceDTO{
		ID:          e.ID,
		Source:      string(e.SourceType),
		URL:         url,
		Observation: e.Observation,
		Reliability: e.Reliability.Final,
		CollectedBy: string(e.CollectedBy),
		Timestamp:   e.CreatedAt.Format(time.RFC3339),
	}
}

func FromClaim(c *entity.Claim) ClaimDTO {
	return ClaimDTO{
		ID:          c.ID,
		Text:        c.Statement,
		Supports:    c.Supports,
		Contradicts: c.Contradicts,
		CreatedBy:   string(c.CreatedBy),
	}
}

func FromVote(v *entity.Vote, agentCode string) VoteDTO {
	return VoteDTO{
		ID:         v.ID,
		AgentCode:  agentCode,
		Stance:     string(v.Decision),
		Confidence: entity.NormalizeConfidence(v.Confidence),
		Reasoning:  v.ReasoningSummary,
		Round:      v.Round,
	}
}

func FromToolCall(t *entity.ToolCall) ToolCallDTO {
	return ToolCallDTO{
		ToolCallID: t.ToolCallID,
		ToolName:   t.ToolName,
		Arguments:  t.Arguments,
		Result:     t.Result,
		Err:        t.Err,
		EvidenceID: t.EvidenceID,
		DurationMs: t.DurationMs,
	}
}

type CreateDatasetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type DatasetResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ItemCount   int    `json:"item_count"`
	CreatedAt   string `json:"created_at"`
}

type DatasetListResponse struct {
	Datasets []DatasetResponse `json:"datasets"`
}

type AddDatasetItemsRequest struct {
	Items []DatasetItemDTO `json:"items"`
}

type DatasetItemDTO struct {
	Question         string          `json:"question"`
	Background       string          `json:"background,omitempty"`
	Constraints      []ConstraintDTO `json:"constraints,omitempty"`
	ExpectedDecision string          `json:"expected_decision"`
	Weight           float64         `json:"weight,omitempty"`
	Tags             []string        `json:"tags,omitempty"`
}

type BenchmarkRunResponse struct {
	ID               string  `json:"id"`
	DatasetID        string  `json:"dataset_id"`
	Status           string  `json:"status"`
	Total            int     `json:"total"`
	Matched          int     `json:"matched"`
	Accuracy         float64 `json:"accuracy"`
	WeightedAccuracy float64 `json:"weighted_accuracy"`
	StartedAt        string  `json:"started_at"`
	CompletedAt      string  `json:"completed_at,omitempty"`
}

type BenchmarkItemResultResponse struct {
	ID               string  `json:"id"`
	CaseID           string  `json:"case_id"`
	ExpectedDecision string  `json:"expected_decision"`
	ActualDecision   string  `json:"actual_decision"`
	Matched          bool    `json:"matched"`
	Score            float64 `json:"score"`
	Error            string  `json:"error,omitempty"`
	Feedback         string  `json:"feedback,omitempty"`
	FeedbackAt       string  `json:"feedback_at,omitempty"`
}

type BenchmarkDetailResponse struct {
	Run     BenchmarkRunResponse          `json:"run"`
	Results []BenchmarkItemResultResponse `json:"results"`
}

func FromDataset(d *entity.BenchmarkDataset) DatasetResponse {
	return DatasetResponse{
		ID: d.ID, Name: d.Name, Description: d.Description,
		ItemCount: d.ItemCount, CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}

func FromBenchmarkRun(r *entity.BenchmarkRun) BenchmarkRunResponse {
	resp := BenchmarkRunResponse{
		ID: r.ID, DatasetID: r.DatasetID, Status: string(r.Status),
		Total: r.Total, Matched: r.Matched, Accuracy: r.Accuracy,
		WeightedAccuracy: r.WeightedAccuracy, StartedAt: r.StartedAt.Format(time.RFC3339),
	}
	if r.CompletedAt != nil {
		resp.CompletedAt = r.CompletedAt.Format(time.RFC3339)
	}
	return resp
}

func FromBenchmarkResult(r *entity.BenchmarkItemResult) BenchmarkItemResultResponse {
	out := BenchmarkItemResultResponse{
		ID: r.ID, CaseID: r.CaseID, ExpectedDecision: string(r.ExpectedDecision),
		ActualDecision: string(r.ActualDecision), Matched: r.Matched, Score: r.Score, Error: r.Error,
		Feedback: r.Feedback,
	}
	if r.FeedbackAt != nil {
		out.FeedbackAt = r.FeedbackAt.Format(time.RFC3339)
	}
	return out
}

type PluginBindingResponse struct {
	ID       string `json:"id"`
	PluginID int64  `json:"plugin_id"`
	ToolID   int64  `json:"tool_id"`
	IsDraft  bool   `json:"is_draft"`
	Enabled  bool   `json:"enabled"`
}

func FromPluginBinding(b *entity.PluginBinding) PluginBindingResponse {
	return PluginBindingResponse{
		ID: b.ID, PluginID: b.PluginID, ToolID: b.ToolID, IsDraft: b.IsDraft, Enabled: b.Enabled,
	}
}

type AdminUsageRow struct {
	UserID  int64   `json:"user_id"`
	Cases   int     `json:"cases"`
	Runs    int     `json:"runs"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

type AdminUsageResponse struct {
	TotalCases   int             `json:"total_cases"`
	TotalRuns    int             `json:"total_runs"`
	TotalTokens  int64           `json:"total_tokens"`
	TotalCostUSD float64         `json:"total_cost_usd"`
	ByUser       []AdminUsageRow `json:"by_user"`
}

func FromAdminUsage(s *admin.UsageSummary) AdminUsageResponse {
	out := AdminUsageResponse{TotalCases: s.TotalCases, TotalRuns: s.TotalRuns, TotalTokens: s.TotalTokens, TotalCostUSD: s.TotalCostUSD}
	for _, r := range s.ByUser {
		out.ByUser = append(out.ByUser, AdminUsageRow{UserID: r.UserID, Cases: r.Cases, Runs: r.Runs, Tokens: r.Tokens, CostUSD: r.CostUSD})
	}
	return out
}

type CreateRecurringRequest struct {
	Name            string          `json:"name"`
	Question        string          `json:"question"`
	Background      string          `json:"background,omitempty"`
	Constraints     []ConstraintDTO `json:"constraints,omitempty"`
	IntervalSeconds int             `json:"interval_seconds"`
}

type SetRecurringEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

type RecurringResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Question        string `json:"question"`
	Background      string `json:"background"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
	LastRunAt       string `json:"last_run_at,omitempty"`
	CreatedAt       string `json:"created_at"`
}

func FromRecurring(r *entity.RecurringCase) RecurringResponse {
	resp := RecurringResponse{
		ID: r.ID, Name: r.Name, Question: r.Question, Background: r.Background,
		IntervalSeconds: int(r.Interval.Seconds()), Enabled: r.Enabled,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
	if r.LastRunAt != nil {
		resp.LastRunAt = r.LastRunAt.Format(time.RFC3339)
	}
	return resp
}

type AskRequest struct {
	Message     string          `json:"message"`
	Background  string          `json:"background,omitempty"`
	Constraints []ConstraintDTO `json:"constraints,omitempty"`
}

type AskResponse struct {
	CaseID   string `json:"case_id"`
	Status   string `json:"status"`
	Decision string `json:"decision"`
	Report   string `json:"report"`
}

func FromAsk(c *entity.DecisionCase, res *entity.Resolution) AskResponse {
	out := AskResponse{CaseID: c.ID, Status: string(c.Status)}
	if res != nil {
		out.Decision = string(res.FinalDecision)
		out.Report = res.FinalReport
	}
	return out
}

type MemorySearchResponse struct {
	Results []*entity.CaseMemoryProjection `json:"results"`
}
