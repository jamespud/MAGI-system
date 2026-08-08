package server

import (
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/handler"
)

// RouteDeps holds all application services needed by the router.
type RouteDeps struct {
	Decision     *decision.Service
	Assistant    *assistant.Service
	Auth         *auth.Service
	Admin        *admin.Service
	Metrics      *metrics.Registry
	Dataset      *dataset.Service
	Plugins      *plugins.Service
	Recurring    *recurring.Service
	Replay       *replay.Service
	Evaluation   *evaluation.Service
	Memory       *memory.Service
	Tool         *tool.Service
	Broker       *EventBroker
	EventRepo    port.EventRepository
	HealthPinger handler.Pinger
	Tracing      *trace.TracerProvider
}

// RegisterRoutesWithDeps registers all HTTP routes with injected services.
func RegisterRoutesWithDeps(h *hzserver.Hertz, deps RouteDeps) {
	h.Use(RequestID(), Tracing(deps.Tracing), Logger(), Recovery(), Metrics(deps.Metrics), Auth(deps.Auth))
	h.GET("/metrics", MetricsHandler(deps.Metrics))

	healthH := handler.NewHealthHandler(deps.HealthPinger)
	h.GET("/health", healthH.Health)
	h.GET("/ready", healthH.Ready)
	h.GET("/version", healthH.Version)
	h.GET("/openapi.json", OpenAPIHandler)

	v1 := h.Group("/api/v1")

	decH := handler.NewDecisionHandler(deps.Decision, deps.Metrics)
	v1.POST("/cases", decH.Create)
	v1.POST("/cases/:id/run", decH.Run)
	v1.POST("/cases/:id/cancel", decH.Cancel)
	v1.GET("/cases/:id", decH.Get)
	v1.GET("/cases/:id/report", decH.Report)
	v1.GET("/cases", decH.List)

	artH := handler.NewArtifactHandler(deps.Decision)
	v1.GET("/cases/:id/agents", artH.Agents)
	v1.GET("/cases/:id/evidence", artH.Evidence)
	v1.GET("/cases/:id/claims", artH.Claims)
	v1.GET("/cases/:id/votes", artH.Votes)

	repH := handler.NewReplayHandler(deps.Replay, deps.Decision)
	v1.GET("/cases/:id/events", repH.Events)
	v1.GET("/cases/:id/timeline", repH.Timeline)
	v1.GET("/cases/:id/trace", repH.Trace)
	v1.GET("/cases/:id/stream", SSEHandlerWithHistory(deps.Broker, deps.EventRepo, deps.Decision))

	memH := handler.NewMemoryHandler(deps.Memory, deps.Decision)
	v1.GET("/memory", memH.Search)
	v1.GET("/memory/:id", memH.Get)

	evalH := handler.NewEvaluationHandler(deps.Evaluation, deps.Decision)
	v1.POST("/evaluation", evalH.Evaluate)
	v1.POST("/evaluation/:id", evalH.Evaluate)
	v1.POST("/benchmark", evalH.Benchmark)

	toolH := handler.NewToolHandler(deps.Tool)
	v1.GET("/tools", toolH.List)
	v1.GET("/tools/:name", toolH.Get)

	dsH := handler.NewDatasetHandler(deps.Dataset)
	v1.POST("/datasets", dsH.Create)
	v1.GET("/datasets", dsH.List)
	v1.GET("/datasets/:id", dsH.Get)
	v1.POST("/datasets/:id/items", dsH.AddItems)
	v1.GET("/datasets/:id/items", dsH.ListItems)
	v1.POST("/datasets/:id/runs", dsH.Run)
	v1.GET("/datasets/:id/runs", dsH.ListRuns)
	v1.GET("/benchmarks/:runID", dsH.RunDetail)
	v1.PATCH("/benchmarks/:runID/results/:resultID", dsH.AddFeedback)

	plugH := handler.NewPluginsHandler(deps.Plugins)
	v1.GET("/plugins", plugH.List)
	v1.POST("/plugins", plugH.Create)
	v1.PATCH("/plugins/:id", plugH.SetEnabled)
	v1.DELETE("/plugins/:id", plugH.Delete)

	adminH := handler.NewAdminHandler(deps.Admin)
	v1.GET("/admin/usage", RequireRole("admin"), adminH.Usage)

	recH := handler.NewRecurringHandler(deps.Recurring)
	v1.GET("/recurring", recH.List)
	v1.POST("/recurring", recH.Create)
	v1.GET("/recurring/:id", recH.Get)
	v1.PATCH("/recurring/:id", recH.SetEnabled)
	v1.DELETE("/recurring/:id", recH.Delete)
	v1.POST("/recurring/:id/run", recH.RunNow)

	askH := handler.NewAssistantHandler(deps.Assistant)
	v1.POST("/assistant", askH.Ask)
}
