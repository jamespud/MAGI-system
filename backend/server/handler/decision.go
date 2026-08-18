package handler

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
	domainservice "github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/server/dto"
)

type DecisionHandler struct {
	svc     *decision.Service
	metrics *metrics.Registry
}

func (h *DecisionHandler) dissent(ctx context.Context, id string, res *entity.Resolution) []entity.Dissent {
	if res == nil {
		return nil
	}
	votes, _ := h.svc.Votes(ctx, id)
	runs, _ := h.svc.AgentRuns(ctx, id)
	codes := make(map[string]entity.MagiCode, len(runs))
	for _, r := range runs {
		codes[r.ID] = r.MagiCode
	}
	return domainservice.ExtractDissent(res.FinalDecision, votes, codes)
}

func NewDecisionHandler(svc *decision.Service, regs ...*metrics.Registry) *DecisionHandler {
	var reg *metrics.Registry
	if len(regs) > 0 {
		reg = regs[0]
	}
	return &DecisionHandler{svc: svc, metrics: reg}
}

func (h *DecisionHandler) Create(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateCaseRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	constraints := make([]entity.Constraint, len(req.Constraints))
	for i, ct := range req.Constraints {
		constraints[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
	}
	case_, err := h.svc.Create(ctx, CurrentUserID(ctx), req.Question, req.Background, constraints)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	h.metrics.IncCasesCreated()
	c.JSON(consts.StatusCreated, dto.FromCase(case_, nil))
}

func (h *DecisionHandler) Run(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	if err := h.svc.StartRun(ctx, case_); err != nil {
		if errors.Is(err, decision.ErrAlreadyRunning) {
			c.JSON(consts.StatusConflict, dto.ErrorResponse{Error: "case already running"})
			return
		}
		if errors.Is(err, decision.ErrBudgetExceeded) {
			c.JSON(consts.StatusTooManyRequests, dto.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusAccepted, dto.CaseResponse{ID: case_.ID, Status: string(case_.Status)})
}

func (h *DecisionHandler) Fork(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	src, err := h.svc.Get(ctx, id)
	if err != nil || src == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, src.UserID) {
		Forbidden(c)
		return
	}
	forked, err := h.svc.ForkAndRun(ctx, CurrentUserID(ctx), id)
	if err != nil {
		if errors.Is(err, decision.ErrBudgetExceeded) {
			c.JSON(consts.StatusTooManyRequests, dto.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusAccepted, dto.FromCase(forked, nil))
}

func (h *DecisionHandler) Cancel(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err == nil && case_ != nil && !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	if h.svc.CancelRun(id) {
		_ = h.svc.Cancel(ctx, id) // also persist CANCELLED status if repo configured
		c.JSON(consts.StatusOK, dto.CaseResponse{ID: id, Status: "CANCELLED"})
		return
	}
	c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "no active run for case"})
}

func (h *DecisionHandler) Get(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: err.Error()})
		return
	}
	if case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	res, _ := h.svc.Resolution(ctx, id)
	c.JSON(consts.StatusOK, dto.FromCaseWithDissent(case_, res, h.dissent(ctx, id, res)))
}

func (h *DecisionHandler) Report(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	res, _ := h.svc.Resolution(ctx, id)
	report := h.svc.Report(ctx, id)
	if ds := h.dissent(ctx, id, res); len(ds) > 0 {
		report += "\n\n--- Dissent ---\n" + domainservice.RenderDissent(ds)
	}
	c.JSON(consts.StatusOK, dto.DecisionReport{Report: report})
}

// List returns one page of cases owned by the caller (P2 D10). Pagination is
// applied in the database (scoped to the caller's user id), not after loading
// every row.
func (h *DecisionHandler) List(ctx context.Context, c *app.RequestContext) {
	page, pageSize := 1, 20
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			pageSize = n
		}
	}
	cases, total, err := h.svc.ListPaged(ctx, CurrentUserID(ctx), page, pageSize)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.CaseResponse, 0, len(cases))
	for _, cs := range cases {
		out = append(out, dto.FromCase(cs, nil))
	}
	c.JSON(consts.StatusOK, dto.CaseListResponse{Cases: out, Total: total, Page: page, PageSize: pageSize})
}

// Patch updates list-management flags (pinned/archived) on a case (P2 D11).
func (h *DecisionHandler) Patch(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	var req dto.UpdateCaseRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid request: " + err.Error()})
		return
	}
	if err := h.svc.UpdateFlags(ctx, id, req.Pinned, req.Archived); err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	updated, err := h.svc.Get(ctx, id)
	if err != nil || updated == nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: "case not found after update"})
		return
	}
	res, _ := h.svc.Resolution(ctx, id)
	c.JSON(consts.StatusOK, dto.FromCaseWithDissent(updated, res, h.dissent(ctx, id, res)))
}

// Delete removes a case and its persisted artifacts (P2 D16).
func (h *DecisionHandler) Delete(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	case_, err := h.svc.Get(ctx, id)
	if err != nil || case_ == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "case not found"})
		return
	}
	if !AuthorizeCase(ctx, case_.UserID) {
		Forbidden(c)
		return
	}
	if h.svc.CancelRun(id) {
		_ = h.svc.Cancel(ctx, id)
	}
	if err := h.svc.Delete(ctx, id); err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]string{"id": id, "status": "deleted"})
}
