package dto

import (
	"encoding/json"
	"time"

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
		sum += v.Confidence
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
