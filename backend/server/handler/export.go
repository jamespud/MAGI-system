package handler

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	domainservice "github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/server/dto"
)

// ExportHandler serves full-data export endpoints (P2 D15). Exports are
// ownership-checked the same way the read endpoints are.
type ExportHandler struct {
	dec    *decision.Service
	events port.EventRepository
	mem    *memory.Service
	eval   *evaluation.Service
	judge  *judge.Service
}

func NewExportHandler(
	dec *decision.Service,
	events port.EventRepository,
	mem *memory.Service,
	eval *evaluation.Service,
	judge *judge.Service,
) *ExportHandler {
	return &ExportHandler{dec: dec, events: events, mem: mem, eval: eval, judge: judge}
}

// Case exports the full audit bundle for one case (P2 D15).
func (h *ExportHandler) Case(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.dec.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !CaseAllowed(ctx, case_) {
		Forbidden(c)
		return
	}
	res, _ := h.dec.Resolution(ctx, id)
	agents, _ := h.dec.AgentRuns(ctx, id)
	evidence, _ := h.dec.Evidence(ctx, id)
	claims, _ := h.dec.Claims(ctx, id)
	votes, _ := h.dec.Votes(ctx, id)
	toolCalls, _ := h.dec.ToolCalls(ctx, id)
	var events []*entity.MagiEvent
	if h.events != nil {
		events, _ = h.events.ListByCase(ctx, id)
	}
	var memory *entity.CaseMemoryProjection
	if h.mem != nil {
		memory, _ = h.mem.Get(ctx, id)
	}
	report := h.dec.Report(ctx, id)
	if ds := h.dissent(ctx, id, res); len(ds) > 0 {
		report += "\n\n--- Dissent ---\n" + domainservice.RenderDissent(ds)
	}
	c.JSON(consts.StatusOK, dto.CaseExport{
		Case:       dto.FromCaseWithDissent(case_, res, h.dissent(ctx, id, res)),
		Resolution: dto.FromResolutionExport(res),
		Report:     report,
		Agents:     agents,
		Evidence:   evidence,
		Claims:     claims,
		Votes:      votes,
		ToolCalls:  toolCalls,
		Events:     events,
		Memory:     memory,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// Memory exports every memory projection the caller may access (P2 D15).
func (h *ExportHandler) Memory(ctx context.Context, c *app.RequestContext) {
	limit := 0
	if h.mem == nil {
		c.JSON(consts.StatusOK, dto.MemoryExport{Results: []*entity.CaseMemoryProjection{}, ExportedAt: time.Now().UTC().Format(time.RFC3339)})
		return
	}
	projs, err := h.mem.ListOwn(ctx, CurrentUserID(ctx), limit)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.MemoryExport{Results: projs, ExportedAt: time.Now().UTC().Format(time.RFC3339)})
}

// Evaluation exports a case's quantitative evaluation plus the latest LLM
// judge verdict (P2 D15).
func (h *ExportHandler) Evaluation(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.dec.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !CaseAllowed(ctx, case_) {
		Forbidden(c)
		return
	}
	var ev *entity.Evaluation
	if h.eval != nil {
		ev, _ = h.eval.EvaluateCase(ctx, id)
	}
	var jr *entity.JudgeResult
	if h.judge != nil {
		jr, _ = h.judge.Latest(ctx, id)
	}
	c.JSON(consts.StatusOK, dto.EvaluationExport{
		CaseID:     id,
		Evaluation: ev,
		Judge:      jr,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// dissent is a package-level helper to avoid importing the domain dissent
// renderer here twice; it reuses the same extraction the decision handler uses.
func (h *ExportHandler) dissent(ctx context.Context, id string, res *entity.Resolution) []entity.Dissent {
	if res == nil {
		return nil
	}
	votes, _ := h.dec.Votes(ctx, id)
	runs, _ := h.dec.AgentRuns(ctx, id)
	codes := make(map[string]entity.MagiCode, len(runs))
	for _, r := range runs {
		codes[r.ID] = r.MagiCode
	}
	return domainservice.ExtractDissent(res.FinalDecision, votes, codes)
}
