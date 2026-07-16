package dto

import (
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type CreateCaseRequest struct {
	Question string `json:"question"`
}

type CaseResponse struct {
	ID            string `json:"id"`
	Question      string `json:"question"`
	Status        string `json:"status"`
	FinalDecision string `json:"final_decision,omitempty"`
	Round         int    `json:"round,omitempty"`
}

type DecisionReport struct {
	Report string `json:"report"`
}

type ReplayEvent struct {
	Type      string    `json:"type"`
	AgentCode string    `json:"agent_code,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
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

func FromCase(c *entity.DecisionCase) CaseResponse {
	return CaseResponse{
		ID:       c.ID,
		Question: c.Question,
		Status:   string(c.Status),
	}
}

func FromResolution(r *entity.Resolution) CaseResponse {
	return CaseResponse{
		FinalDecision: string(r.FinalDecision),
		Round:         r.Consensus.Round,
	}
}

func FromEvent(e *entity.MagiEvent) ReplayEvent {
	code := ""
	if e.AgentCode != nil {
		code = string(*e.AgentCode)
	}
	return ReplayEvent{
		Type:      string(e.Type),
		AgentCode: code,
		RunID:     e.RunID,
		Timestamp: e.Timestamp,
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
