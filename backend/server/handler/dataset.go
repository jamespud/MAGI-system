package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

type DatasetHandler struct {
	svc *dataset.Service
}

func NewDatasetHandler(svc *dataset.Service) *DatasetHandler {
	return &DatasetHandler{svc: svc}
}

func (h *DatasetHandler) Create(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateDatasetRequest
	if err := c.BindAndValidate(&req); err != nil || req.Name == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "name is required"})
		return
	}
	d, err := h.svc.Create(ctx, CurrentUserID(ctx), req.Name, req.Description)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusCreated, dto.FromDataset(d))
}

func (h *DatasetHandler) List(ctx context.Context, c *app.RequestContext) {
	datasets, err := h.svc.List(ctx, CurrentUserID(ctx))
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.DatasetResponse, 0, len(datasets))
	for _, d := range datasets {
		out = append(out, dto.FromDataset(d))
	}
	c.JSON(consts.StatusOK, dto.DatasetListResponse{Datasets: out})
}

func (h *DatasetHandler) Get(ctx context.Context, c *app.RequestContext) {
	d, err := h.svc.Get(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil || d == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "dataset not found"})
		return
	}
	c.JSON(consts.StatusOK, dto.FromDataset(d))
}

func (h *DatasetHandler) AddItems(ctx context.Context, c *app.RequestContext) {
	var req dto.AddDatasetItemsRequest
	if err := c.BindAndValidate(&req); err != nil || len(req.Items) == 0 {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "items is required"})
		return
	}
	items := make([]dataset.NewItem, 0, len(req.Items))
	for _, it := range req.Items {
		constraints := make([]entity.Constraint, len(it.Constraints))
		for i, ct := range it.Constraints {
			constraints[i] = entity.Constraint{Key: ct.Label, Value: ct.Value}
		}
		items = append(items, dataset.NewItem{
			Question: it.Question, Background: it.Background, Constraints: constraints,
			ExpectedDecision: entity.VoteDecision(it.ExpectedDecision), Weight: it.Weight, Tags: it.Tags,
		})
	}
	count, err := h.svc.AddItems(ctx, CurrentUserID(ctx), c.Param("id"), items)
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, map[string]int{"added": count})
}

func (h *DatasetHandler) ListItems(ctx context.Context, c *app.RequestContext) {
	items, err := h.svc.ListItems(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.DatasetItemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, dto.DatasetItemDTO{
			Question: it.Question, Background: it.Context,
			ExpectedDecision: string(it.ExpectedDecision), Weight: it.Weight, Tags: it.Tags,
		})
	}
	c.JSON(consts.StatusOK, out)
}

// Run enqueues an asynchronous dataset benchmark and returns the run id.
// Poll GET /api/v1/benchmarks/:runID for progress and results.
func (h *DatasetHandler) Run(ctx context.Context, c *app.RequestContext) {
	runs := 0
	if v := c.Query("runs"); v != "" {
		fmt.Sscanf(v, "%d", &runs)
	}
	threshold := 0.0
	if v := c.Query("threshold"); v != "" {
		fmt.Sscanf(v, "%f", &threshold)
	}
	run, err := h.svc.StartRunWithOptions(ctx, CurrentUserID(ctx), c.Param("id"), dataset.RunOptions{RunsPerItem: runs, RegressionThreshold: threshold})
	if err != nil {
		if errors.Is(err, dataset.ErrRunActive) {
			c.JSON(consts.StatusConflict, dto.ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusAccepted, dto.FromBenchmarkRun(run))
}

func (h *DatasetHandler) ListRuns(ctx context.Context, c *app.RequestContext) {
	runs, err := h.svc.ListRuns(ctx, CurrentUserID(ctx), c.Param("id"))
	if err != nil {
		c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]dto.BenchmarkRunResponse, 0, len(runs))
	for _, r := range runs {
		out = append(out, dto.FromBenchmarkRun(r))
	}
	c.JSON(consts.StatusOK, out)
}

// AddFeedback records user feedback on a benchmark item result.
func (h *DatasetHandler) AddFeedback(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Feedback string `json:"feedback"`
	}
	if err := c.BindAndValidate(&req); err != nil || req.Feedback == "" {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "feedback is required"})
		return
	}
	if err := h.svc.AddFeedback(ctx, CurrentUserID(ctx), c.Param("runID"), c.Param("resultID"), req.Feedback); err != nil {
		c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(consts.StatusOK, dto.ErrorResponse{Error: ""})
}

func (h *DatasetHandler) RunDetail(ctx context.Context, c *app.RequestContext) {
	run, results, err := h.svc.RunDetail(ctx, CurrentUserID(ctx), c.Param("runID"))
	if err != nil || run == nil {
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "benchmark run not found"})
		return
	}
	out := make([]dto.BenchmarkItemResultResponse, 0, len(results))
	for _, r := range results {
		out = append(out, dto.FromBenchmarkResult(r))
	}
	c.JSON(consts.StatusOK, dto.BenchmarkDetailResponse{Run: dto.FromBenchmarkRun(run), Results: out})
}
