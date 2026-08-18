package server

import (
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/application/knowledge"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/handler"
)

// RouteDeps holds all application services needed by the router.
type RouteDeps struct {
	Decision        *decision.Service
	Approval        *approval.Service
	Assistant       *assistant.Service
	Auth            *auth.Service
	Admin           *admin.Service
	Metrics         *metrics.Registry
	Dataset         *dataset.Service
	Plugins         *plugins.Service
	Recurring       *recurring.Service
	Replay          *replay.Service
	SelfImprove     *handler.SelfImproveHandler
	RolePolicy      *handler.RolePolicyHandler
	Golden          *handler.GoldenHandler
	ConsensusPolicy *handler.ConsensusPolicyHandler
	FSMBlueprint    *handler.FSMBlueprintHandler
	TaskTree        *handler.TaskTreeHandler
	Evaluation      *evaluation.Service
	Judge           *judge.Service
	Memory          *memory.Service
	Knowledge       *knowledge.Service
	Users           *users.Service
	OIDC            *handler.OIDCHandler
	Tool            *tool.Service
	Broker          *EventBroker
	EventRepo       port.EventRepository
	Export          *handler.ExportHandler
	HealthPinger    handler.Pinger
	ModelName       string
	MaxSteps        int
	Tracing         *trace.TracerProvider
	// RateLimit configures per-minute HTTP rate limiting on /api/v1 (P2 D13).
	RateLimit RateLimitConfig
	// MetricsAuth requires the admin role for /metrics when true (P2 D17).
	MetricsAuth bool
	// MaxTokensPerUser / MaxCostUSDPerUser feed /me/usage budget display (P2 D9).
	MaxTokensPerUser  int64
	MaxCostUSDPerUser float64
	// PromptRepo backs the admin prompt registry (P2 D12).
	PromptRepo port.PromptRepository
}

// RegisterRoutesWithDeps registers all HTTP routes with injected services.
func RegisterRoutesWithDeps(h *hzserver.Hertz, deps RouteDeps) {
	h.Use(RequestID(), Tracing(deps.Tracing), Logger(), Recovery(), Metrics(deps.Metrics), Auth(deps.Auth))
	if deps.MetricsAuth {
		// /metrics normally sits in publicPaths; drop it so Auth runs first,
		// then gate the handler on the admin role (open mode still passes).
		delete(publicPaths, "/metrics")
		h.GET("/metrics", RequireRole("admin"), MetricsHandler(deps.Metrics))
	} else {
		h.GET("/metrics", MetricsHandler(deps.Metrics))
	}

	healthH := handler.NewHealthHandler(deps.HealthPinger)
	h.GET("/health", healthH.Health)
	h.GET("/ready", healthH.Ready)
	h.GET("/version", healthH.Version)

	if deps.OIDC != nil {
		h.GET("/auth/oidc/login", deps.OIDC.Login)
		h.GET("/auth/oidc/callback", deps.OIDC.Callback)
		h.POST("/auth/register", deps.OIDC.Register)
	}
	h.GET("/openapi.json", OpenAPIHandler)

	v1 := h.Group("/api/v1")
	v1.Use(RateLimit(deps.RateLimit))

	decH := handler.NewDecisionHandler(deps.Decision, deps.Metrics)
	v1.POST("/cases", decH.Create)
	v1.POST("/cases/:id/run", decH.Run)
	v1.POST("/cases/:id/fork", decH.Fork)
	v1.POST("/cases/:id/cancel", decH.Cancel)
	v1.POST("/cases/:id/pause", decH.Pause)
	v1.POST("/cases/:id/resume", decH.Resume)
	v1.GET("/cases/:id", decH.Get)
	v1.PATCH("/cases/:id", decH.Patch)
	v1.DELETE("/cases/:id", decH.Delete)
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
	if deps.TaskTree != nil {
		v1.GET("/cases/:id/task-tree", deps.TaskTree.List)
	}
	v1.GET("/cases/:id/stream", SSEHandlerWithHistory(deps.Broker, deps.EventRepo, deps.Decision))

	if deps.SelfImprove != nil {
		v1.POST("/admin/selfimprove/analyze", RequireAnyRole("admin", "operator"), deps.SelfImprove.Analyze)
		v1.GET("/admin/selfimprove/suggestions", RequireAnyRole("admin", "operator"), deps.SelfImprove.List)
		v1.POST("/admin/selfimprove/suggestions/:id/apply", RequireRole("admin"), deps.SelfImprove.Apply)
	}
	if deps.RolePolicy != nil {
		v1.GET("/admin/role-policies", RequireAnyRole("admin", "operator"), deps.RolePolicy.List)
		v1.PUT("/admin/role-policies/:code", RequireAnyRole("admin", "operator"), deps.RolePolicy.Update)
		v1.POST("/admin/role-policies/:code/reset", RequireAnyRole("admin", "operator"), deps.RolePolicy.Reset)
	}
	if deps.Golden != nil {
		v1.POST("/admin/golden", RequireAnyRole("admin", "operator"), deps.Golden.Add)
		v1.GET("/admin/golden", RequireAnyRole("admin", "operator"), deps.Golden.List)
		v1.DELETE("/admin/golden/:id", RequireAnyRole("admin", "operator"), deps.Golden.Delete)
		v1.POST("/admin/golden/sync", RequireAnyRole("admin", "operator"), deps.Golden.Sync)
	}
	if deps.ConsensusPolicy != nil {
		v1.GET("/admin/consensus-policy", RequireAnyRole("admin", "operator"), deps.ConsensusPolicy.Get)
		v1.PUT("/admin/consensus-policy", RequireAnyRole("admin", "operator"), deps.ConsensusPolicy.Update)
		v1.POST("/admin/consensus-policy/reset", RequireAnyRole("admin", "operator"), deps.ConsensusPolicy.Reset)
	}
	if deps.FSMBlueprint != nil {
		v1.GET("/admin/fsm-blueprint", RequireAnyRole("admin", "operator"), deps.FSMBlueprint.Get)
		v1.PUT("/admin/fsm-blueprint", RequireAnyRole("admin", "operator"), deps.FSMBlueprint.Update)
		v1.POST("/admin/fsm-blueprint/reset", RequireAnyRole("admin", "operator"), deps.FSMBlueprint.Reset)
		v1.POST("/admin/fsm-blueprint/validate", RequireAnyRole("admin", "operator"), deps.FSMBlueprint.Validate)
	}

	memH := handler.NewMemoryHandler(deps.Memory, deps.Decision)
	v1.GET("/memory", memH.Search)
	v1.GET("/memory/:id", memH.Get)
	v1.PATCH("/memory/:id", memH.Update)
	v1.DELETE("/memory/:id", memH.Delete)

	knowH := handler.NewKnowledgeHandler(deps.Knowledge)
	v1.POST("/knowledge", knowH.Create)
	v1.GET("/knowledge", knowH.List)
	v1.GET("/knowledge/:id", knowH.Get)
	v1.DELETE("/knowledge/:id", knowH.Delete)

	evalH := handler.NewEvaluationHandler(deps.Evaluation, deps.Decision)
	v1.POST("/evaluation", evalH.Evaluate)
	v1.POST("/evaluation/:id", evalH.Evaluate)

	judgeH := handler.NewJudgeHandler(deps.Judge, deps.Decision)
	v1.GET("/evaluation/:id/judge", judgeH.Get)
	v1.POST("/evaluation/:id/judge", judgeH.Judge)
	v1.POST("/benchmark", evalH.Benchmark)

	toolH := handler.NewToolHandler(deps.Tool, deps.Plugins)
	v1.GET("/tools", toolH.List)
	v1.GET("/tools/:name", toolH.Get)
	statusH := handler.NewStatusHandler(deps.Metrics, deps.ModelName, deps.MaxSteps)
	v1.GET("/status", statusH.Status)

	apprH := handler.NewApprovalHandler(deps.Approval, deps.Decision)
	v1.GET("/approvals", apprH.List)
	v1.GET("/approvals/:id", apprH.Get)
	v1.POST("/approvals/:id/approve", apprH.Approve)
	v1.POST("/approvals/:id/reject", apprH.Reject)

	dsH := handler.NewDatasetHandler(deps.Dataset)
	v1.POST("/datasets", dsH.Create)
	v1.GET("/datasets", dsH.List)
	v1.GET("/datasets/:id", dsH.Get)
	v1.DELETE("/datasets/:id", dsH.Delete)
	v1.POST("/datasets/:id/items", dsH.AddItems)
	v1.GET("/datasets/:id/items", dsH.ListItems)
	v1.PATCH("/datasets/:id/items/:itemId", dsH.UpdateItem)
	v1.DELETE("/datasets/:id/items/:itemId", dsH.DeleteItem)
	v1.GET("/datasets/:id/items/export", dsH.ExportItems)
	v1.POST("/datasets/:id/runs", dsH.Run)
	v1.GET("/datasets/:id/runs", dsH.ListRuns)
	v1.GET("/benchmarks/:runID", dsH.RunDetail)
	v1.PATCH("/benchmarks/:runID/results/:resultID", dsH.AddFeedback)
	v1.POST("/admin/benchmarks/seed", RequireAnyRole("admin", "operator"), dsH.SeedBuiltin)
	v1.GET("/admin/eval/summary", RequireAnyRole("admin", "operator"), dsH.EvalSummary)

	plugH := handler.NewPluginsHandler(deps.Plugins)
	v1.GET("/plugins", plugH.List)
	v1.POST("/plugins", plugH.Create)
	v1.PATCH("/plugins/:id", plugH.SetEnabled)
	v1.DELETE("/plugins/:id", plugH.Delete)

	adminH := handler.NewAdminHandler(deps.Admin, admin.UsageLimits{
		MaxTokens: deps.MaxTokensPerUser, MaxCostUSD: deps.MaxCostUSDPerUser,
	})
	v1.GET("/admin/usage", RequireAnyRole("admin", "operator"), adminH.Usage)
	v1.GET("/me/usage", adminH.MeUsage)

	if deps.PromptRepo != nil {
		promptH := handler.NewPromptHandler(deps.PromptRepo)
		v1.GET("/admin/prompts", RequireAnyRole("admin", "operator"), promptH.List)
		v1.GET("/admin/prompts/:key", RequireAnyRole("admin", "operator"), promptH.Get)
		v1.PUT("/admin/prompts/:key", RequireAnyRole("admin", "operator"), promptH.Update)
		v1.POST("/admin/prompts/:key/restore", RequireAnyRole("admin", "operator"), promptH.Restore)
	}

	usersH := handler.NewUsersHandler(deps.Users)
	v1.GET("/me", usersH.Me)
	v1.POST("/me/keys", usersH.IssueOwnKey)
	v1.POST("/admin/users", RequireRole("admin"), usersH.CreateUser)
	v1.GET("/admin/users", RequireRole("admin"), usersH.ListUsers)
	v1.DELETE("/admin/users/:id", RequireRole("admin"), usersH.DeleteUser)
	v1.GET("/admin/users/:id/keys", RequireRole("admin"), usersH.ListKeys)
	v1.POST("/admin/users/:id/keys", RequireRole("admin"), usersH.IssueKey)
	v1.POST("/admin/keys/:id/revoke", RequireRole("admin"), usersH.RevokeKey)
	v1.POST("/admin/keys/:id/rotate", RequireRole("admin"), usersH.RotateKey)

	recH := handler.NewRecurringHandler(deps.Recurring)
	v1.GET("/recurring", recH.List)
	v1.POST("/recurring", recH.Create)
	v1.GET("/recurring/:id", recH.Get)
	v1.PATCH("/recurring/:id", recH.SetEnabled)
	v1.DELETE("/recurring/:id", recH.Delete)
	v1.POST("/recurring/:id/run", recH.RunNow)

	askH := handler.NewAssistantHandler(deps.Assistant)
	convH := handler.NewConversationHandler(deps.Assistant)
	v1.POST("/assistant", askH.Ask)
	v1.GET("/conversations", convH.List)
	v1.GET("/conversations/:id", convH.Get)
	v1.DELETE("/conversations/:id", convH.Delete)

	if deps.Export != nil {
		v1.GET("/cases/:id/export", deps.Export.Case)
		v1.GET("/memory/export", deps.Export.Memory)
		v1.GET("/evaluation/:id/export", deps.Export.Evaluation)
	}
}
