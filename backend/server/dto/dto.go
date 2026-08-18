package dto

import (
	"encoding/json"
	"time"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/dataset"

	"github.com/jamespud/magi/backend/domain/consensus"
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
	ParentCaseID  string          `json:"parent_case_id,omitempty"`
	Status        string          `json:"status"`
	Consensus     *ConsensusDTO   `json:"consensus,omitempty"`
	Dissent       []DissentDTO    `json:"dissent,omitempty"`
	FinalDecision string          `json:"final_decision,omitempty"`
	Confidence    float64         `json:"confidence"`
	Round         int             `json:"round"`
	Pinned        bool            `json:"pinned"`
	Archived      bool            `json:"archived"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

// UpdateCaseRequest carries optional flag updates for PATCH /cases/:id.
type UpdateCaseRequest struct {
	Pinned   *bool `json:"pinned"`
	Archived *bool `json:"archived"`
}

// CaseListResponse now carries pagination metadata (P2 D10).
type CaseListResponse struct {
	Cases    []CaseResponse `json:"cases"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
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
	ApprovedBy string `json:"approved_by,omitempty"`
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
	Seq       uint64          `json:"seq,omitempty"`
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

type ApprovalRequestDTO struct {
	ID          string `json:"id"`
	CaseID      string `json:"case_id"`
	RunID       string `json:"run_id,omitempty"`
	AgentCode   string `json:"agent_code,omitempty"`
	ToolName    string `json:"tool_name"`
	Arguments   string `json:"arguments,omitempty"`
	Status      string `json:"status"`
	Reason      string `json:"reason,omitempty"`
	DecidedBy   string `json:"decided_by,omitempty"`
	RequestedAt string `json:"requested_at,omitempty"`
	DecidedAt   string `json:"decided_at,omitempty"`
}

type ApprovalListResponse struct {
	Approvals []ApprovalRequestDTO `json:"approvals"`
}

type ApprovalDecisionRequest struct {
	Reason string `json:"reason,omitempty"`
}

func FromApproval(a *entity.ApprovalRequest) ApprovalRequestDTO {
	out := ApprovalRequestDTO{
		ID: a.ID, CaseID: a.CaseID, RunID: a.RunID, AgentCode: string(a.AgentCode),
		ToolName: a.ToolName, Arguments: a.Arguments, Status: string(a.Status),
		Reason: a.Reason, DecidedBy: a.DecidedBy,
		RequestedAt: a.RequestedAt.Format(time.RFC3339),
	}
	if a.DecidedAt != nil {
		out.DecidedAt = a.DecidedAt.Format(time.RFC3339)
	}
	return out
}

func FromCase(c *entity.DecisionCase, resolution *entity.Resolution) CaseResponse {
	return fromCase(c, resolution, nil)
}

func FromCaseWithDissent(c *entity.DecisionCase, resolution *entity.Resolution, dissent []entity.Dissent) CaseResponse {
	return fromCase(c, resolution, dissent)
}

func fromCase(c *entity.DecisionCase, resolution *entity.Resolution, dissent []entity.Dissent) CaseResponse {
	constraints := make([]ConstraintDTO, len(c.Constraints))
	for i, ct := range c.Constraints {
		constraints[i] = ConstraintDTO{Label: ct.Key, Value: ct.Value}
	}
	resp := CaseResponse{
		ID:           c.ID,
		Question:     c.Question,
		Background:   c.Context,
		Constraints:  constraints,
		Status:       string(c.Status),
		ParentCaseID: c.ParentCaseID,
		Round:        0,
		Pinned:       c.Pinned,
		Archived:     c.Archived,
		CreatedAt:    c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.Format(time.RFC3339),
	}
	if len(dissent) > 0 {
		for _, d := range dissent {
			resp.Dissent = append(resp.Dissent, FromDissent(d))
		}
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
		Seq:       e.Seq,
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
	case entity.EventToolApprovalRequested:
		return "Tool approval requested"
	case entity.EventToolApprovalResolved:
		return "Tool approval resolved"
	case entity.EventContextCompacted:
		return "Context compacted"
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
		ApprovedBy: t.ApprovedBy,
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

// EvalDatasetRow is one dataset's aggregate row in the eval summary.
type EvalDatasetRow struct {
	DatasetID    string  `json:"dataset_id"`
	Name         string  `json:"name"`
	Runs         int     `json:"runs"`
	AvgAccuracy  float64 `json:"avg_accuracy"`
	AvgStability float64 `json:"avg_stability"`
}

// EvalRunRow is one finished benchmark run in the summary.
type EvalRunRow struct {
	RunID            string  `json:"run_id"`
	DatasetID        string  `json:"dataset_id"`
	DatasetName      string  `json:"dataset_name"`
	Status           string  `json:"status"`
	Accuracy         float64 `json:"accuracy"`
	Stability        float64 `json:"stability"`
	RegressionFailed bool    `json:"regression_failed"`
	CompletedAt      string  `json:"completed_at,omitempty"`
}

// EvalSummaryResponse is the aggregate evaluation dashboard payload.
type EvalSummaryResponse struct {
	TotalRuns            int              `json:"total_runs"`
	SucceededRuns        int              `json:"succeeded_runs"`
	FailedRuns           int              `json:"failed_runs"`
	AvgAccuracy          float64          `json:"avg_accuracy"`
	AvgStability         float64          `json:"avg_stability"`
	RegressionFailedRuns int              `json:"regression_failed_runs"`
	Datasets             []EvalDatasetRow `json:"datasets"`
	RecentRuns           []EvalRunRow     `json:"recent_runs"`
}

func FromEvalSummary(s *dataset.EvalSummary) EvalSummaryResponse {
	if s == nil {
		return EvalSummaryResponse{}
	}
	out := EvalSummaryResponse{
		TotalRuns: s.TotalRuns, SucceededRuns: s.SucceededRuns, FailedRuns: s.FailedRuns,
		AvgAccuracy: s.AvgAccuracy, AvgStability: s.AvgStability, RegressionFailedRuns: s.RegressionFailedRuns,
	}
	for _, d := range s.Datasets {
		if d == nil {
			continue
		}
		out.Datasets = append(out.Datasets, EvalDatasetRow{
			DatasetID: d.DatasetID, Name: d.Name, Runs: d.Runs,
			AvgAccuracy: d.AvgAccuracy, AvgStability: d.AvgStability,
		})
	}
	for _, r := range s.RecentRuns {
		row := EvalRunRow{
			RunID: r.RunID, DatasetID: r.DatasetID, DatasetName: r.DatasetName,
			Status: string(r.Status), Accuracy: r.Accuracy, Stability: r.Stability,
			RegressionFailed: r.RegressionFailed,
		}
		if r.CompletedAt != nil {
			row.CompletedAt = r.CompletedAt.Format(time.RFC3339)
		}
		out.RecentRuns = append(out.RecentRuns, row)
	}
	return out
}

// AnalyzeSelfImproveRequest asks the harness to analyze one failed case.
type AnalyzeSelfImproveRequest struct {
	CaseID string `json:"case_id"`
}

// SelfImproveSuggestionResponse is one analyzed failure suggestion.
type SelfImproveSuggestionResponse struct {
	ID            string `json:"id"`
	CaseID        string `json:"case_id"`
	RunID         string `json:"run_id,omitempty"`
	AgentCode     string `json:"agent_code,omitempty"`
	Category      string `json:"category"`
	Failure       string `json:"failure"`
	Summary       string `json:"summary"`
	SuggestedRule string `json:"suggested_rule"`
	PromptKey     string `json:"prompt_key,omitempty"`
	PromptContent string `json:"prompt_content,omitempty"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	AppliedAt     string `json:"applied_at,omitempty"`
}

func FromSelfImprove(s *entity.SelfImproveSuggestion) SelfImproveSuggestionResponse {
	out := SelfImproveSuggestionResponse{
		ID: s.ID, CaseID: s.CaseID, RunID: s.RunID, AgentCode: s.AgentCode,
		Category: s.Category, Failure: s.Failure, Summary: s.Summary,
		SuggestedRule: s.SuggestedRule, PromptKey: s.PromptKey, PromptContent: s.PromptContent,
		Status: s.Status, CreatedAt: s.CreatedAt.Format(time.RFC3339),
	}
	if s.AppliedAt != nil {
		out.AppliedAt = s.AppliedAt.Format(time.RFC3339)
	}
	return out
}

// RolePolicyDTO is the editable role-contract specification.
type RolePolicyDTO struct {
	Role                    string  `json:"role"`
	EnforceAssessment       bool    `json:"enforce_assessment"`
	RequiredAssessment      string  `json:"required_assessment"`
	MaxResidualRisk         float64 `json:"max_residual_risk"`
	MinTechnicalScore       float64 `json:"min_technical_score"`
	MinOpportunityScore     float64 `json:"min_opportunity_score"`
	MinWeightedUtilityScore float64 `json:"min_weighted_utility_score"`
	DebateDirective         string  `json:"debate_directive"`
}

func FromRolePolicy(p *entity.RolePolicy) RolePolicyDTO {
	if p == nil {
		return RolePolicyDTO{}
	}
	return RolePolicyDTO{
		Role: p.Role, EnforceAssessment: p.EnforceAssessment,
		RequiredAssessment: p.RequiredAssessment, MaxResidualRisk: p.MaxResidualRisk,
		MinTechnicalScore: p.MinTechnicalScore, MinOpportunityScore: p.MinOpportunityScore,
		MinWeightedUtilityScore: p.MinWeightedUtilityScore, DebateDirective: p.DebateDirective,
	}
}

func (d RolePolicyDTO) ToEntity() entity.RolePolicy {
	return entity.RolePolicy{
		Role: d.Role, EnforceAssessment: d.EnforceAssessment,
		RequiredAssessment: d.RequiredAssessment, MaxResidualRisk: d.MaxResidualRisk,
		MinTechnicalScore: d.MinTechnicalScore, MinOpportunityScore: d.MinOpportunityScore,
		MinWeightedUtilityScore: d.MinWeightedUtilityScore, DebateDirective: d.DebateDirective,
	}
}

// AddGoldenRequest promotes a completed production case to the golden set.
type AddGoldenRequest struct {
	CaseID string `json:"case_id"`
}

// GoldenCaseResponse is one online-golden decision case.
type GoldenCaseResponse struct {
	ID               string `json:"id"`
	CaseID           string `json:"case_id"`
	Question         string `json:"question"`
	Context          string `json:"context"`
	ExpectedDecision string `json:"expected_decision"`
	CreatedAt        string `json:"created_at"`
}

func FromGoldenCase(g *entity.GoldenCase) GoldenCaseResponse {
	return GoldenCaseResponse{
		ID: g.ID, CaseID: g.CaseID, Question: g.Question, Context: g.Context,
		ExpectedDecision: string(g.ExpectedDecision), CreatedAt: g.CreatedAt.Format(time.RFC3339),
	}
}

// TaskNodeDTO is one node of the case task state tree.
type TaskNodeDTO struct {
	ID          string `json:"id"`
	CaseID      string `json:"case_id"`
	ParentID    string `json:"parent_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
	CreatedAt   string `json:"created_at"`
	CompletedAt string `json:"completed_at,omitempty"`
}

func FromTaskNode(n *entity.TaskNode) TaskNodeDTO {
	out := TaskNodeDTO{
		ID: n.ID, CaseID: n.CaseID, ParentID: n.ParentID, RunID: n.RunID,
		Kind: n.Kind, Title: n.Title, Status: n.Status, Detail: n.Detail,
		CreatedAt: n.CreatedAt.Format(time.RFC3339),
	}
	if n.CompletedAt != nil {
		out.CompletedAt = n.CompletedAt.Format(time.RFC3339)
	}
	return out
}

// ConsensusPolicyDTO is the editable consensus/voting rule set.
type ConsensusPolicyDTO struct {
	Quorum                      int  `json:"quorum"`
	FirstSplitGoesToDebate      bool `json:"first_split_goes_to_debate"`
	ResolveOnReconsiderMajority bool `json:"resolve_on_reconsider_majority"`
	ConditionalAsApprove        bool `json:"conditional_as_approve"`
}

func FromConsensusPolicy(p consensus.ConsensusPolicy) ConsensusPolicyDTO {
	return ConsensusPolicyDTO{
		Quorum: p.Quorum, FirstSplitGoesToDebate: p.FirstSplitGoesToDebate,
		ResolveOnReconsiderMajority: p.ResolveOnReconsiderMajority, ConditionalAsApprove: p.ConditionalAsApprove,
	}
}

func (d ConsensusPolicyDTO) ToEntity() consensus.ConsensusPolicy {
	return consensus.ConsensusPolicy{
		Quorum: d.Quorum, FirstSplitGoesToDebate: d.FirstSplitGoesToDebate,
		ResolveOnReconsiderMajority: d.ResolveOnReconsiderMajority, ConditionalAsApprove: d.ConditionalAsApprove,
	}
}

type AddDatasetItemsRequest struct {
	Items []DatasetItemDTO `json:"items"`
}

type DatasetItemDTO struct {
	ID               string          `json:"id,omitempty"`
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
	RunsPerItem      int     `json:"runs_per_item"`
	Stability        float64 `json:"stability"`
	RegressionFailed bool    `json:"regression_failed"`
	FailureReason    string  `json:"failure_reason,omitempty"`
	StartedAt        string  `json:"started_at"`
	CompletedAt      string  `json:"completed_at,omitempty"`
}

type BenchmarkItemResultResponse struct {
	ID               string   `json:"id"`
	CaseID           string   `json:"case_id"`
	ExpectedDecision string   `json:"expected_decision"`
	ActualDecision   string   `json:"actual_decision"`
	Matched          bool     `json:"matched"`
	Score            float64  `json:"score"`
	Runs             int      `json:"runs"`
	Consistency      float64  `json:"consistency"`
	Decisions        []string `json:"decisions,omitempty"`
	Error            string   `json:"error,omitempty"`
	Feedback         string   `json:"feedback,omitempty"`
	FeedbackAt       string   `json:"feedback_at,omitempty"`
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
		WeightedAccuracy: r.WeightedAccuracy, RunsPerItem: r.RunsPerItem, Stability: r.Stability,
		RegressionFailed: r.RegressionFailed, FailureReason: r.FailureReason, StartedAt: r.StartedAt.Format(time.RFC3339),
	}
	if r.CompletedAt != nil {
		resp.CompletedAt = r.CompletedAt.Format(time.RFC3339)
	}
	return resp
}

func FromItem(it *entity.BenchmarkItem) DatasetItemDTO {
	constraints := make([]ConstraintDTO, len(it.Constraints))
	for i, ct := range it.Constraints {
		constraints[i] = ConstraintDTO{Label: ct.Key, Value: ct.Value}
	}
	return DatasetItemDTO{
		ID: it.ID, Question: it.Question, Background: it.Context, Constraints: constraints,
		ExpectedDecision: string(it.ExpectedDecision), Weight: it.Weight, Tags: it.Tags,
	}
}

type StatusResponse struct {
	ModelName   string  `json:"model_name"`
	MaxSteps    int     `json:"max_steps"`
	TokensTotal int64   `json:"tokens_total"`
	CostUSD     float64 `json:"cost_usd"`
	RunsActive  int64   `json:"runs_active"`
	Connected   bool    `json:"connected"`
}

func FromBenchmarkResult(r *entity.BenchmarkItemResult) BenchmarkItemResultResponse {
	out := BenchmarkItemResultResponse{
		ID: r.ID, CaseID: r.CaseID, ExpectedDecision: string(r.ExpectedDecision),
		ActualDecision: string(r.ActualDecision), Matched: r.Matched, Score: r.Score, Runs: r.Runs, Consistency: r.Consistency, Error: r.Error,
		Feedback: r.Feedback,
	}
	if r.FeedbackAt != nil {
		out.FeedbackAt = r.FeedbackAt.Format(time.RFC3339)
	}
	for _, d := range r.Decisions {
		out.Decisions = append(out.Decisions, string(d))
	}
	return out
}

type JudgeResultDTO struct {
	CaseID              string  `json:"case_id"`
	ReportQuality       float64 `json:"report_quality"`
	EvidenceConsistency float64 `json:"evidence_consistency"`
	ReflectionValidity  float64 `json:"reflection_validity"`
	Overall             float64 `json:"overall"`
	Rationale           string  `json:"rationale,omitempty"`
	ModelName           string  `json:"model_name,omitempty"`
	CreatedAt           string  `json:"created_at"`
}

func FromJudge(r *entity.JudgeResult) JudgeResultDTO {
	return JudgeResultDTO{
		CaseID: r.CaseID, ReportQuality: r.ReportQuality, EvidenceConsistency: r.EvidenceConsistency,
		ReflectionValidity: r.ReflectionValidity, Overall: r.Overall, Rationale: r.Rationale,
		ModelName: r.ModelName, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

type DissentDTO struct {
	AgentCode   string   `json:"agent_code"`
	Decision    string   `json:"decision"`
	Reasoning   string   `json:"reasoning,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	ClaimIDs    []string `json:"claim_ids,omitempty"`
	Conditions  []string `json:"conditions,omitempty"`
}

func FromDissent(d entity.Dissent) DissentDTO {
	out := DissentDTO{
		AgentCode: string(d.AgentCode), Decision: string(d.Decision), Reasoning: d.Reasoning,
		EvidenceIDs: d.EvidenceIDs, ClaimIDs: d.ClaimIDs,
	}
	for _, c := range d.Conditions {
		out.Conditions = append(out.Conditions, c.Statement)
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

// MeUsageResponse is the authenticated user's own usage and budget (P2 D9).
type MeUsageResponse struct {
	UserID         int64   `json:"user_id"`
	Cases          int     `json:"cases"`
	Runs           int     `json:"runs"`
	Tokens         int64   `json:"tokens"`
	CostUSD        float64 `json:"cost_usd"`
	MaxTokens      int64   `json:"max_tokens"`   // 0 = unlimited
	MaxCostUSD     float64 `json:"max_cost_usd"` // 0 = unlimited
	TokensExceeded bool    `json:"tokens_exceeded"`
	CostExceeded   bool    `json:"cost_exceeded"`
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
	Message        string          `json:"message"`
	Background     string          `json:"background,omitempty"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Constraints    []ConstraintDTO `json:"constraints,omitempty"`
}

type MemorySearchResponse struct {
	Results []*entity.CaseMemoryProjection `json:"results"`
}

// MemoryUpdateRequest edits user-owned long-term-memory metadata and display
// fields. Nil pointers preserve fields; an empty tags array clears tags.
type MemoryUpdateRequest struct {
	QuestionSummary *string  `json:"question_summary,omitempty"`
	ContextSummary  *string  `json:"context_summary,omitempty"`
	Resolution      *string  `json:"resolution,omitempty"`
	Learned         *string  `json:"learned,omitempty"`
	Annotation      *string  `json:"annotation,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

// KnowledgeDocDTO is the API representation of a knowledge document.
type KnowledgeDocDTO struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	SourceKind string `json:"source_kind"`
	SourceURL  string `json:"source_url,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Chunks     int    `json:"chunks"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type CreateKnowledgeRequest struct {
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
}

type KnowledgeListResponse struct {
	Documents []KnowledgeDocDTO `json:"documents"`
	Total     int               `json:"total"`
}

// FromKnowledgeDoc maps a knowledge document entity to its DTO.
func FromKnowledgeDoc(d *entity.KnowledgeDoc) KnowledgeDocDTO {
	out := KnowledgeDocDTO{
		ID: d.ID, Title: d.Title, SourceKind: d.SourceKind, SourceURL: d.SourceURL,
		Status: d.Status, Error: d.Error, Chunks: d.Chunks,
	}
	if !d.CreatedAt.IsZero() {
		out.CreatedAt = d.CreatedAt.Format(time.RFC3339)
	}
	if !d.UpdatedAt.IsZero() {
		out.UpdatedAt = d.UpdatedAt.Format(time.RFC3339)
	}
	return out
}

// UserDTO is the API representation of a harness account.
type UserDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	ActiveKeys int    `json:"active_keys"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// ApiKeyDTO is the API representation of a stored API key (no hash, no secret).
type ApiKeyDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	Revoked    bool    `json:"revoked"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
}

// IssuedKeyDTO carries a freshly issued API key. Plaintext is only returned
// at issuance.
type IssuedKeyDTO struct {
	ID        string `json:"id"`
	Prefix    string `json:"prefix"`
	Plaintext string `json:"plaintext"`
}

type CreateUserRequest struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type CreateUserResponse struct {
	User   UserDTO       `json:"user"`
	ApiKey *IssuedKeyDTO `json:"api_key,omitempty"`
}

type IssueKeyRequest struct {
	Name string `json:"name,omitempty"`
}

type MeResponse struct {
	User UserDTO     `json:"user"`
	Keys []ApiKeyDTO `json:"keys"`
}

type UserListResponse struct {
	Users []UserDTO `json:"users"`
}

type KeyListResponse struct {
	Keys []ApiKeyDTO `json:"keys"`
}

// FromUser maps a user entity (plus key count) to its DTO.
func FromUser(u *entity.User, activeKeys int) UserDTO {
	out := UserDTO{ID: u.ID, Name: u.Name, Role: u.Role, ActiveKeys: activeKeys}
	if !u.CreatedAt.IsZero() {
		out.CreatedAt = u.CreatedAt.Format(time.RFC3339)
	}
	return out
}

// FromApiKey maps an API key entity to its DTO (never exposes the hash).
func FromApiKey(k *entity.ApiKey) ApiKeyDTO {
	out := ApiKeyDTO{ID: k.ID, Name: k.Name, Prefix: k.Prefix, Revoked: k.Revoked}
	if k.LastUsedAt != nil {
		lu := k.LastUsedAt.Format(time.RFC3339)
		out.LastUsedAt = &lu
	}
	if !k.CreatedAt.IsZero() {
		out.CreatedAt = k.CreatedAt.Format(time.RFC3339)
	}
	return out
}

// --- P2 D15: Export DTOs ---

// CaseExport is the full audit bundle for one case.
type CaseExport struct {
	Case       CaseResponse                 `json:"case"`
	Resolution *ResolutionExport            `json:"resolution,omitempty"`
	Report     string                       `json:"report,omitempty"`
	Agents     []*entity.AgentRun           `json:"agents"`
	Evidence   []*entity.EvidenceRecord     `json:"evidence"`
	Claims     []*entity.Claim              `json:"claims"`
	Votes      []*entity.Vote               `json:"votes"`
	ToolCalls  []*entity.ToolCall           `json:"tool_calls"`
	Events     []*entity.MagiEvent          `json:"events"`
	Memory     *entity.CaseMemoryProjection `json:"memory,omitempty"`
	ExportedAt string                       `json:"exported_at"`
}

// ResolutionExport is a JSON-friendly view of the resolution.
type ResolutionExport struct {
	ID             string   `json:"id"`
	CaseID         string   `json:"case_id"`
	Outcome        string   `json:"outcome"`
	Round          int      `json:"round"`
	Detail         string   `json:"detail"`
	FinalDecision  string   `json:"final_decision"`
	FinalReport    string   `json:"final_report"`
	KeyEvidenceIDs []string `json:"key_evidence_ids"`
	KeyClaimIDs    []string `json:"key_claim_ids"`
	VoteIDs        []string `json:"vote_ids"`
	Conditions     []string `json:"conditions,omitempty"`
	CreatedAt      string   `json:"created_at"`
}

// FromResolutionExport maps a resolution to its export DTO (nil-safe).
func FromResolutionExport(r *entity.Resolution) *ResolutionExport {
	if r == nil {
		return nil
	}
	cond := make([]string, 0, len(r.Consensus.Conditions))
	for _, cd := range r.Consensus.Conditions {
		cond = append(cond, cd.Statement)
	}
	return &ResolutionExport{
		ID: r.ID, CaseID: r.CaseID, Outcome: string(r.Consensus.Outcome),
		Round: r.Consensus.Round, Detail: r.Consensus.Detail,
		FinalDecision: string(r.FinalDecision), FinalReport: r.FinalReport,
		KeyEvidenceIDs: r.KeyEvidenceIDs, KeyClaimIDs: r.KeyClaimIDs, VoteIDs: r.VoteIDs,
		Conditions: cond, CreatedAt: r.CreatedAt.Format(time.RFC3339),
	}
}

// MemoryExport is the full memory projection list for a user.
type MemoryExport struct {
	Results    []*entity.CaseMemoryProjection `json:"results"`
	ExportedAt string                         `json:"exported_at"`
}

// EvaluationExport bundles quantitative evaluation and the latest judge verdict.
type EvaluationExport struct {
	CaseID     string              `json:"case_id"`
	Evaluation *entity.Evaluation  `json:"evaluation,omitempty"`
	Judge      *entity.JudgeResult `json:"judge,omitempty"`
	ExportedAt string              `json:"exported_at"`
}

// --- P2 D12: Prompt registry DTOs ---

type PromptDTO struct {
	Key       string `json:"key"`
	Version   int    `json:"version"`
	Active    bool   `json:"active"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updated_at"`
}

type PromptListResponse struct {
	Prompts []PromptDTO `json:"prompts"`
}

type UpdatePromptRequest struct {
	Content string `json:"content"`
}

func FromPrompt(t *entity.PromptTemplate) PromptDTO {
	return PromptDTO{
		Key: t.Key, Version: t.Version, Active: t.Active, Content: t.Content,
		UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
}

// ConversationDTO is the API representation of a conversation thread.
type ConversationDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ConversationMessageDTO is one turn of a conversation.
type ConversationMessageDTO struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CaseID    string `json:"case_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

type ConversationListResponse struct {
	Conversations []ConversationDTO `json:"conversations"`
}

type ConversationDetailResponse struct {
	Conversation ConversationDTO          `json:"conversation"`
	Messages     []ConversationMessageDTO `json:"messages"`
}

type AskInConversationResponse struct {
	CaseResponse
	ConversationID string `json:"conversation_id,omitempty"`
}

// FromConversation maps a conversation entity to its DTO.
func FromConversation(c *entity.Conversation) ConversationDTO {
	return ConversationDTO{
		ID: c.ID, Title: c.Title,
		CreatedAt: c.CreatedAt.Format(time.RFC3339), UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

// FromConversationMessage maps a conversation message entity to its DTO.
func FromConversationMessage(m *entity.ConversationMessage) ConversationMessageDTO {
	return ConversationMessageDTO{
		ID: m.ID, Role: m.Role, Content: m.Content, CaseID: m.CaseID,
		CreatedAt: m.CreatedAt.Format(time.RFC3339),
	}
}
