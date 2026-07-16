package server

import (
	hzserver "github.com/cloudwego/hertz/pkg/app/server"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/server/handler"
)

// RouteDeps holds all application services needed by the router.
type RouteDeps struct {
	Decision   *decision.Service
	Replay     *replay.Service
	Evaluation *evaluation.Service
	Memory     *memory.Service
	Tool       *tool.Service
}

// RegisterRoutesWithDeps registers all HTTP routes with injected services.
func RegisterRoutesWithDeps(h *hzserver.Hertz, deps RouteDeps) {
	h.Use(RequestID(), Recovery())

	healthH := handler.NewHealthHandler()
	h.GET("/health", healthH.Health)
	h.GET("/ready", healthH.Ready)
	h.GET("/version", healthH.Version)

	v1 := h.Group("/api/v1")

	decH := handler.NewDecisionHandler(deps.Decision)
	v1.POST("/cases", decH.Create)
	v1.POST("/cases/:id/run", decH.Run)
	v1.POST("/cases/:id/cancel", decH.Cancel)
	v1.GET("/cases/:id", decH.Get)
	v1.GET("/cases/:id/report", decH.Report)
	v1.GET("/cases", nopHandler("list cases"))

	repH := handler.NewReplayHandler(deps.Replay)
	v1.GET("/cases/:id/events", repH.Events)
	v1.GET("/cases/:id/timeline", repH.Timeline)
	v1.GET("/cases/:id/trace", nopHandler("trace"))

	memH := handler.NewMemoryHandler(deps.Memory)
	v1.GET("/memory/:id", memH.Get)

	evalH := handler.NewEvaluationHandler(deps.Evaluation)
	v1.POST("/evaluation", evalH.Evaluate)
	v1.POST("/benchmark", nopHandler("benchmark"))

	toolH := handler.NewToolHandler(deps.Tool)
	v1.GET("/tools", toolH.List)
	v1.GET("/tools/:name", toolH.Get)
}
