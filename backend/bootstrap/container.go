package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	crossplugin "github.com/coze-dev/coze-studio/backend/crossdomain/plugin"
	crossworkflow "github.com/coze-dev/coze-studio/backend/crossdomain/workflow"
	coderunnersandbox "github.com/coze-dev/coze-studio/backend/infra/coderunner/impl/sandbox"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	magi "github.com/jamespud/magi/backend/adapter"
	mcpadapter "github.com/jamespud/magi/backend/adapter/mcp"
	rag "github.com/jamespud/magi/backend/adapter/rag"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/consensuspolicy"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/fsmblueprint"
	"github.com/jamespud/magi/backend/application/golden"
	"github.com/jamespud/magi/backend/application/investigationplan"
	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/application/knowledge"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/ragindex"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/rolepolicy"
	"github.com/jamespud/magi/backend/application/selfimprove"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/application/toolquota"
	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	"github.com/jamespud/magi/backend/domain/execution"
	domainmemory "github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/modelruntime"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	promptpkg "github.com/jamespud/magi/backend/domain/prompt"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/toolruntime"
	"github.com/jamespud/magi/backend/domain/validation"
	appserver "github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/a2a"
	"github.com/jamespud/magi/backend/server/handler"
)

// Module is the Uber Fx module that wires all MAGI dependencies.
var Module = fx.Options(
	fx.Provide(
		// Validation
		validation.NewReflectSchemaGenerator,
		validation.NewJSONSchemaValidator,

		// Adapters (standalone mode; Coze mode replaces these)
		magi.NewModelAdapterWithMetrics,
		appserver.NewEventBroker,
		provideEventPublisher,
		provideContextBuilder,
		ProvideToolRegistry,
		ProvideToolExecutor,
		provideMCPAdapter,
		provideAuthService,
		provideSessionCodec,
		provideSessionAuthorizer,
		provideOIDCClient,
		provideOIDCHandler,
		provideUserRepository,
		provideApiKeyRepository,
		provideUsersService,
		ProvideKnowledgePort,

		// Database
		provideDB,
		provideRepository,
		providePromptRepository,
		provideRolePolicyRepository,
		provideRolePolicyService,
		provideRolePolicyHandler,
		provideGoldenRepository,
		provideGoldenService,
		provideGoldenHandler,
		provideConsensusPolicyRepository,
		provideConsensusPolicyService,
		provideConsensusPolicyHandler,
		provideFSMBlueprintRepository,
		provideFSMBlueprintService,
		provideFSMBlueprintHandler,
		provideCaseRepository,
		provideTaskTreeRepository,
		provideTaskTreeRecorder,
		provideTaskTreeHandler,
		provideInvestigationPlanRepository,
		provideInvestigationPlanService,
		provideInvestigationPlanHandler,
		provideAuditRepository,
		provideAuditService,
		provideAuditHandler,
		providePromptProvider,
		provideDecisionJobRepository,
		provideDatasetRepository,
		providePluginBindingRepository,
		provideApprovalRepository,
		provideApprovalService,
		provideJudgeRepository,
		provideJudgeService,
		provideSchedulerLock,
		provideToolQuotaRepository,
		provideToolQuotaService,
		provideToolRuntime,
		provideBudgetChecker,
		provideRuntimeInvocationRepository,
		provideModelRuntime,

		// Agent runtime
		provideAgentLoopHolder,
		provideAgentLoop,
		provideCommander,
		provideMagiConfigs,
		provideOrchestrator,

		// Application
		provideRunManager,
		provideDecisionService,
		provideReplayService,
		provideSelfImproveRepository,
		provideSelfImproveService,
		provideSelfImproveHandler,
		provideEvaluationService,
		provideMemoryService,
		provideRagIndexJobRepository,
		provideRagIndexService,
		provideRagIndexPoller,
		provideAdminRagHandler,
		provideKnowledgeRepository,
		provideKnowledgeService,
		provideToolService,
		provideDatasetService,
		providePluginsService,
		provideAdminService,
		provideRecurringRepository,
		provideRecurringService,
		provideConversationRepository,
		provideAssistantService,
		metrics.New,
		provideToolPolicy,
		provideRedactor,
		provideTracingProvider,
		provideHealthPinger,
		ProvideA2A,

		// Server
		provideServer,
	),
	fx.Invoke(func(
		h *hzserver.Hertz,
		cfg *Config,
		apprSvc *approval.Service,
		judgeSvc *judge.Service,
		decSvc *decision.Service,
		authSvc *auth.Service,
		dsSvc *dataset.Service,
		repSvc *replay.Service,
		siH *handler.SelfImproveHandler,
		rpH *handler.RolePolicyHandler,
		goldenH *handler.GoldenHandler,
		cpH *handler.ConsensusPolicyHandler,
		fbH *handler.FSMBlueprintHandler,
		ttH *handler.TaskTreeHandler,
		ipH *handler.InvestigationPlanHandler,
		auditSvc *audit.Service,
		auditH *handler.AuditHandler,
		evalSvc *evaluation.Service,
		memSvc *memory.Service,
		knowSvc *knowledge.Service,
		usersSvc *users.Service,
		oidcH *handler.OIDCHandler,
		toolSvc *tool.Service,
		broker *appserver.EventBroker,
		repo port.Repository,
		reg *metrics.Registry,
		plugs *plugins.Service,
		admSvc *admin.Service,
		recSvc *recurring.Service,
		askSvc *assistant.Service,
		ragH *handler.AdminRagHandler,
		dbPing func(context.Context) error,
		tp *trace.TracerProvider,
		a2aOpt *A2A,
	) {
		var a2aMount *a2atransport.MountDeps
		if a2aOpt != nil && a2aOpt.Enabled {
			a2aMount = a2aOpt.MountDeps
		}
		appserver.RegisterRoutesWithDeps(h, appserver.RouteDeps{
			Decision:          decSvc,
			Approval:          apprSvc,
			Judge:             judgeSvc,
			Auth:              authSvc,
			Metrics:           reg,
			Dataset:           dsSvc,
			Plugins:           plugs,
			Admin:             admSvc,
			Recurring:         recSvc,
			Assistant:         askSvc,
			Replay:            repSvc,
			SelfImprove:       siH,
			RolePolicy:        rpH,
			Golden:            goldenH,
			AdminRag:          ragH,
			ConsensusPolicy:   cpH,
			FSMBlueprint:      fbH,
			TaskTree:          ttH,
			InvestigationPlan: ipH,
			Audit:             auditSvc,
			AuditH:            auditH,
			Evaluation:        evalSvc,
			Memory:            memSvc,
			Knowledge:         knowSvc,
			Users:             usersSvc,
			OIDC:              oidcH,
			Tool:              toolSvc,
			Broker:            broker,
			EventRepo:         repo.EventRepo(),
			HealthPinger:      dbPing,
			Tracing:           tp,
			ModelName:         cfg.Model.ModelName,
			MaxSteps:          cfg.Magi.MaxSteps,
			Export:            handler.NewExportHandler(decSvc, repo.EventRepo(), memSvc, evalSvc, judgeSvc),
			RateLimit: appserver.RateLimitConfig{
				Enabled:          cfg.HTTPRateLimit.Enabled,
				PerUserPerMinute: cfg.HTTPRateLimit.PerUserPerMinute,
				PerIPPerMinute:   cfg.HTTPRateLimit.PerIPPerMinute,
			},
			MetricsAuth:       cfg.Metrics.AuthRequired,
			MaxTokensPerUser:  cfg.Limits.MaxTokensPerUser,
			MaxCostUSDPerUser: cfg.Limits.MaxCostUSDPerUser,
			PromptRepo:        repo.PromptRepo(),
			A2A:               a2aMount,
			A2ARateLimit: appserver.RateLimitConfig{
				Enabled:          cfg.A2A.Enabled && cfg.HTTPRateLimit.Enabled,
				PerUserPerMinute: cfg.HTTPRateLimit.PerUserPerMinute,
				PerIPPerMinute:   cfg.HTTPRateLimit.PerIPPerMinute,
			},
		})
	}),
	fx.Invoke(registerLifecycle),
	fx.Invoke(registerScheduler),
	fx.Invoke(registerTracingShutdown),
	fx.Invoke(func(a *mcpadapter.Adapter, lc fx.Lifecycle) {
		lc.Append(fx.Hook{OnStop: func(context.Context) error { return a.Close() }})
	}),
)

func provideAgentLoop(
	modelPort *magi.ModelAdapter,
	toolReg port.ToolRegistryPort,
	toolExec port.ToolExecutorPort,
	val validation.Validator,
	gen validation.SchemaGenerator,
	eventPub port.EventPublisher,
	repo port.Repository,
	toolPol *toolpolicy.Policy,
	reg *metrics.Registry,
	red *redact.Redactor,
	approvalRepo port.ApprovalRepository,
	quota *toolquota.Service,
	toolRuntime *toolruntime.Runtime,
	prompts port.PromptProvider,
	taskTree port.TaskTreeRecorder,
	modelRuntime *modelruntime.Runtime,
	loopHolder *agentLoopHolder,
) (*runtime.AgentLoop, error) {
	adapterRegistry := evidence.NewEvidenceAdapterRegistry(
		evidence.FullReliabilityResolver(),
		evidence.NewWebSearchAdapter(),
		evidence.NewNativeAdapter(),
		evidence.NewRawObservationAdapter(),
	)
	loop, err := runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelPort, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen, EventPub: eventPub, CheckpointRepo: repo.CheckpointRepo(), Adapter: adapterRegistry, ToolPolicy: toolPol, Metrics: reg, Redactor: red, ApprovalRepo: approvalRepo, Quota: quota, ToolRuntime: toolRuntime, Prompts: prompts, TaskTree: taskTree, ModelRuntime: modelRuntime,
	})
	if err != nil {
		return nil, err
	}
	loopHolder.set(loop)
	return loop, nil
}

func provideRuntimeInvocationRepository(db *gorm.DB) port.RuntimeInvocationRepository {
	return magi.NewRuntimeInvocationRepository(db)
}

func provideModelRuntime(invocations port.RuntimeInvocationRepository) *modelruntime.Runtime {
	return modelruntime.New(execution.NewKernel(invocations, nil))
}

func provideToolRuntime(
	invocations port.RuntimeInvocationRepository,
	executor port.ToolExecutorPort,
	validator validation.Validator,
	policy *toolpolicy.Policy,
	quota *toolquota.Service,
	registry *metrics.Registry,
	redactor *redact.Redactor,
) (*toolruntime.Runtime, error) {
	return toolruntime.New(toolruntime.Deps{
		Kernel:    execution.NewKernel(invocations, nil),
		Executor:  executor,
		Validator: validator,
		Policy:    policy,
		Quota:     quota,
		Metrics:   registry,
		Redactor:  redactor,
	})
}

func provideTaskTreeRepository(db *gorm.DB) port.TaskTreeRepository {
	return magi.NewTaskTreeRepository(db)
}

func provideCaseRepository(db *gorm.DB) port.CaseRepository {
	return magi.NewRepository(db).CaseRepo()
}

func provideTaskTreeRecorder(db *gorm.DB) port.TaskTreeRecorder {
	// The same taskTreeRepo backs both the repository and the recorder; the
	// loop only needs the narrow write-only surface (see port.TaskTreeRecorder).
	return magi.NewTaskTreeRepository(db)
}

func provideTaskTreeHandler(repo port.TaskTreeRepository, caseSvc port.CaseRepository) *handler.TaskTreeHandler {
	return handler.NewTaskTreeHandler(repo, caseSvc)
}

func provideInvestigationPlanRepository(db *gorm.DB) port.InvestigationPlanRepository {
	return magi.NewInvestigationPlanRepository(db)
}

func provideInvestigationPlanService(repo port.InvestigationPlanRepository) *investigationplan.Service {
	return investigationplan.NewService(repo)
}

func provideInvestigationPlanHandler(svc *investigationplan.Service, caseSvc port.CaseRepository) *handler.InvestigationPlanHandler {
	return handler.NewInvestigationPlanHandler(svc, caseSvc)
}

func provideAuditRepository(db *gorm.DB) port.AuditRepository {
	return magi.NewAuditRepository(db)
}

func provideAuditService(repo port.AuditRepository) *audit.Service {
	return audit.NewService(repo)
}

func provideAuditHandler(svc *audit.Service) *handler.AuditHandler {
	return handler.NewAuditHandler(svc)
}

// ProvideToolRegistry routes local/plugin/workflow/code-runner/MCP bindings through one registry.
func ProvideToolRegistry(cfg *Config, mcpAdapter *mcpadapter.Adapter) port.ToolRegistryPort {
	var local port.ToolRegistryPort
	if enabledLocal := enabledLocalTools(cfg); len(enabledLocal) > 0 {
		local = magi.NewLocalToolRegistry(enabledLocal...)
	}
	var mcpReg port.ToolRegistryPort
	if len(cfg.MCP.Servers) > 0 {
		mcpReg = mcpAdapter
	}
	return magi.NewToolRegistryMuxWithAll(local, magi.NewPluginAdapter(crossplugin.DefaultSVC()),
		magi.NewWorkflowAdapter(crossworkflow.DefaultSVC()), codeRunnerAdapter(cfg), mcpReg)
}

// ProvideToolExecutor routes local/plugin/workflow/code-runner/MCP execution through one executor.
func ProvideToolExecutor(cfg *Config, mcpAdapter *mcpadapter.Adapter, reg *metrics.Registry, val validation.Validator,
	loopHolder *agentLoopHolder, magiConfigs []*entity.MagiConfig) (port.ToolExecutorPort, error) {
	var local port.ToolExecutorPort
	executors := map[string]port.ToolExecutorPort{}
	if providers := webSearchProviderSpecs(cfg); len(providers) > 0 {
		specs := make([]magi.WebSearchProviderSpec, 0, len(providers))
		for _, provider := range providers {
			specs = append(specs, magi.WebSearchProviderSpec{
				Provider: provider.Provider, APIKey: provider.APIKey, BaseURL: provider.BaseURL,
			})
		}
		webSearch, err := magi.NewWebSearchToolExecutor(specs, reg)
		if err != nil {
			return nil, err
		}
		executors["web_search"] = webSearch
	}
	if cfg.DBTool.Enabled {
		dbDriver := cfg.DBTool.Driver
		if dbDriver == "" {
			dbDriver = cfg.Database.Driver
		}
		dbDSN := cfg.DBTool.DSN
		if dbDSN == "" {
			dbDSN = cfg.Database.DSN
		}
		dbTool, err := magi.NewDBQueryToolExecutor(magi.DBQueryToolConfig{
			Enabled: cfg.DBTool.Enabled, Driver: dbDriver, DSN: dbDSN,
			MaxRows: cfg.DBTool.MaxRows, MaxQueryChars: cfg.DBTool.MaxQueryChars,
			TimeoutSeconds: cfg.DBTool.TimeoutSeconds, BlockedPrefixes: cfg.DBTool.BlockedPrefixes,
		})
		if err != nil {
			return nil, err
		}
		executors[magi.DBQueryToolName] = dbTool
	}
	if feedbackToolEnabled(cfg) {
		executors[magi.FeedbackToolName] = magi.NewFeedbackToolExecutor(
			runtime.NewCompositeFeedbackSensor(
				runtime.NewSchemaFeedbackSensor(val),
				runtime.NewConstraintFeedbackSensor(),
			),
			reg,
		)
	}
	if cfg.FileTool.Enabled {
		fileTool, err := magi.NewFileToolExecutor(magi.FileToolConfig{
			Enabled: cfg.FileTool.Enabled, Roots: cfg.FileTool.Roots,
			MaxFileBytes: cfg.FileTool.MaxFileBytes, MaxListItems: cfg.FileTool.MaxListItems,
			AllowWrite: cfg.FileTool.AllowWrite, AllowAppend: cfg.FileTool.AllowAppend,
			AllowDelete: cfg.FileTool.AllowDelete, AllowMkdir: cfg.FileTool.AllowMkdir,
		})
		if err != nil {
			return nil, err
		}
		executors[magi.FileToolName] = fileTool
	}
	if cfg.RepoTool.Enabled {
		repoTool, err := magi.NewRepoQueryToolExecutor(magi.RepoToolConfig{
			Enabled: cfg.RepoTool.Enabled, Roots: cfg.RepoTool.Roots,
			Includes: cfg.RepoTool.Includes, MaxResults: cfg.RepoTool.MaxResults,
			MaxFileBytes: cfg.RepoTool.MaxFileBytes,
		})
		if err != nil {
			return nil, err
		}
		executors[magi.RepoToolName] = repoTool
	}
	if cfg.WebTool.Enabled {
		webTool, err := magi.NewWebFetchToolExecutor(magi.WebFetchToolConfig{
			Enabled: cfg.WebTool.Enabled, AllowedDomains: cfg.WebTool.AllowedDomains,
			MaxBytes: cfg.WebTool.MaxBytes, TimeoutSeconds: cfg.WebTool.TimeoutSeconds,
		})
		if err != nil {
			return nil, err
		}
		executors[magi.WebFetchToolName] = webTool
	}
	if cfg.DelegateTool.Enabled {
		roleCfg := (*entity.MagiConfig)(nil)
		if len(magiConfigs) > 0 {
			roleCfg = magiConfigs[0]
		}
		investigator, err := magi.NewLoopSubInvestigator(loopHolder, roleCfg)
		if err != nil {
			return nil, err
		}
		delegate, err := magi.NewDelegateToolExecutor(investigator, magi.DelegateToolConfig{})
		if err != nil {
			return nil, err
		}
		executors[magi.DelegateToolName] = delegate
	}
	if cfg.SensorTool.Enabled {
		checks := make([]magi.SensorCheck, 0, len(cfg.SensorTool.Checks))
		for _, check := range cfg.SensorTool.Checks {
			checks = append(checks, magi.SensorCheck{
				Name: check.Name, Command: check.Command, Args: check.Args, Timeout: check.Timeout,
			})
		}
		sensorTool, err := magi.NewSensorToolExecutor(magi.SensorToolConfig{Enabled: true, Checks: checks}, nil)
		if err != nil {
			return nil, err
		}
		executors[magi.SensorToolName] = sensorTool
	}
	var err error
	local, err = magi.NewLocalToolMux(executors)
	if err != nil {
		return nil, err
	}
	var mcpExec port.ToolExecutorPort
	if len(cfg.MCP.Servers) > 0 {
		mcpExec = mcpAdapter
	}
	return magi.NewToolExecutorMuxWithAll(local, magi.NewPluginAdapter(crossplugin.DefaultSVC()),
		magi.NewWorkflowAdapter(crossworkflow.DefaultSVC()), codeRunnerAdapter(cfg), mcpExec), nil
}

// provideMCPAdapter builds the MCP client adapter from config. It is always
// non-nil so the lifecycle close hook can be registered; the registry/executor
// mux only attaches it when at least one server is configured.
func provideMCPAdapter(cfg *Config) *mcpadapter.Adapter {
	cfgs := make([]mcpadapter.ServerConfig, 0, len(cfg.MCP.Servers))
	for _, s := range cfg.MCP.Servers {
		cfgs = append(cfgs, mcpadapter.ServerConfig{
			Name: s.Name, Transport: s.Transport, Command: s.Command, Args: s.Args, URL: s.URL,
			Env: s.Env, TimeoutSeconds: s.TimeoutSeconds, Headers: s.Headers, RetryAttempts: s.RetryAttempts,
		})
	}
	return mcpadapter.New(cfgs)
}

// ProvideKnowledgePort builds the HybridKnowledgeAdapter. When milvus.address /
// elasticsearch.addresses are non-empty, real backends are REQUIRED (no fake
// fallback) - connection failure returns an error so misconfiguration is
// visible. When addresses are empty, in-memory fakes are used (tests / pure
// standalone). Store is async when store_async is enabled.
// ProvideKnowledgePort builds the HybridKnowledgeAdapter and returns it both
// as the case-memory KnowledgePort and as the DocumentIndexer used by the
// knowledge service. When async store is enabled, the KnowledgePort is the
// AsyncIndexer while the DocumentIndexer stays the synchronous adapter (doc
// uploads are user-triggered and return their index status immediately).
func ProvideKnowledgePort(cfg *Config, db *gorm.DB, pub port.EventPublisher) (port.KnowledgePort, port.DocumentIndexer, port.MemoryIndexer, error) {
	ch := rag.NewChunker(rag.RuneTokenCounter{CharsPerToken: 4}, rag.ChunkLevels{L1800: 1800, L900: 900, L300: 300})
	emb := rag.NewOpenAIEmbedder(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.ModelName, cfg.Embedding.Dim)

	var vec rag.VectorIndex
	if cfg.Milvus.Address != "" {
		real, err := rag.NewMilvusIndexer(cfg.Milvus.Address, cfg.Milvus.Collection, cfg.Embedding.Dim)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("milvus connect %q: %w (set milvus.address empty to use fake index)", cfg.Milvus.Address, err)
		}
		vec = real
	} else {
		vec = &rag.FakeVectorIndex{}
	}
	var lex rag.LexicalIndex
	if len(cfg.Elasticsearch.Addresses) > 0 {
		real, err := rag.NewESIndexer(cfg.Elasticsearch.Addresses, cfg.Elasticsearch.Index)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("elasticsearch connect %v: %w (set elasticsearch.addresses empty to use fake index)", cfg.Elasticsearch.Addresses, err)
		}
		lex = real
	} else {
		lex = &rag.FakeLexicalIndex{}
	}

	repo := rag.NewChunkRepository(db)
	retriever := rag.NewRetriever(vec, lex, emb, repo, rag.MergeOpts{
		TopK: cfg.RAG.TopK, RRFK: cfg.RAG.RRFK,
		Thr900: cfg.RAG.MergeThreshold900, Thr1800: cfg.RAG.MergeThreshold1800,
		Orphan: cfg.RAG.OrphanStrategy,
	})
	// inner shares the event publisher so storeRaw emits MEMORY_INDEXED on
	// successful indexing (the poller executes synchronously through inner).
	inner := rag.NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever, pub)
	if cfg.RAG.StoreAsync {
		// Durable queue path: all RAG mutations are enqueued into
		// rag_index_job and executed by the RagIndexPoller. The same
		// DurableIndexer satisfies all three port interfaces.
		jobRepo := magi.NewRagIndexJobRepository(db)
		durable := rag.NewDurableIndexer(inner, jobRepo)
		return durable, durable, durable, nil
	}
	// Sync path: the adapter itself satisfies all three interfaces.
	return inner, inner, inner, nil
}

func provideCommander(
	cfg *Config,
	modelPort *magi.ModelAdapter,
	gen validation.SchemaGenerator,
	val validation.Validator,
	prompts port.PromptProvider,
) (*service.Commander, error) {
	return service.NewCommander(
		service.CommanderConfig{
			Model:   cfg.CommanderModelRef(),
			Persona: "commander",
			Prompts: prompts,
		},
		modelPort, gen, val,
	)
}

func provideMagiConfigs(cfg *Config, rolePolicies port.RolePolicyRepository) []*entity.MagiConfig {
	configs := []*entity.MagiConfig{
		cfg.Magi.Melchior.ToConfig("melchior", cfg),
		cfg.Magi.Balthasar.ToConfig("balthasar", cfg),
		cfg.Magi.Casper.ToConfig("casper", cfg),
	}
	for _, config := range configs {
		if config == nil {
			continue
		}
		if stored, err := rolePolicies.Get(context.Background(), string(config.Code)); err == nil && stored != nil {
			config.RolePolicy = *stored
		}
	}
	return configs
}

func provideOrchestrator(
	agentLoop *runtime.AgentLoop,
	commander *service.Commander,
	eventPub port.EventPublisher,
	configs []*entity.MagiConfig,
	repo port.Repository,
	contextBuilder *domainmemory.ContextBuilder,
	knowledge port.KnowledgePort,
	plugs *plugins.Service,
	policyRepo port.ConsensusPolicyRepository,
	blueprintRepo port.FSMBlueprintRepository,
) *orchestration.Orchestrator {
	policy := consensus.DefaultConsensusPolicy()
	if stored, err := policyRepo.Get(context.Background()); err == nil && stored != nil {
		policy = *stored
	}
	blueprint := entity.DefaultFSMBlueprint()
	if stored, err := blueprintRepo.Get(context.Background()); err == nil && stored != nil {
		blueprint = *stored
	}
	return orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop:            agentLoop,
		Consensus:            consensus.NewConsensusEngine(),
		Debate:               debate.NewDebateEngine(nil),
		Commander:            commander,
		EventPub:             eventPub,
		CaseRepo:             repo.CaseRepo(),
		Repo:                 repo,
		ContextBuilder:       contextBuilder,
		Knowledge:            knowledge,
		MemoryRepo:           repo.MemoryRepo(),
		Configs:              configs,
		Policy:               policy,
		Blueprint:            &blueprint,
		ToolBindingsProvider: plugs,
	})
}

func provideConsensusPolicyRepository(db *gorm.DB) port.ConsensusPolicyRepository {
	return magi.NewConsensusPolicyRepository(db)
}

func provideConsensusPolicyService(repo port.ConsensusPolicyRepository) *consensuspolicy.Service {
	return consensuspolicy.NewService(repo)
}

func provideConsensusPolicyHandler(svc *consensuspolicy.Service) *handler.ConsensusPolicyHandler {
	return handler.NewConsensusPolicyHandler(svc)
}

func provideFSMBlueprintRepository(db *gorm.DB) port.FSMBlueprintRepository {
	return magi.NewFSMBlueprintRepository(db)
}

func provideFSMBlueprintService(repo port.FSMBlueprintRepository) *fsmblueprint.Service {
	return fsmblueprint.NewService(repo)
}

func provideFSMBlueprintHandler(svc *fsmblueprint.Service) *handler.FSMBlueprintHandler {
	return handler.NewFSMBlueprintHandler(svc)
}

func provideApprovalRepository(db *gorm.DB) port.ApprovalRepository {
	return magi.NewApprovalRepository(db)
}

func provideApprovalService(repo port.ApprovalRepository) *approval.Service {
	return approval.NewService(repo)
}

func provideJudgeRepository(db *gorm.DB) port.JudgeRepository {
	return magi.NewJudgeRepository(db)
}

func provideJudgeService(cfg *Config, modelPort *magi.ModelAdapter, gen validation.SchemaGenerator, val validation.Validator, repo port.Repository, judgeRepo port.JudgeRepository) (*judge.Service, error) {
	j, err := judge.NewService(modelPort, cfg.JudgeModelRef(), gen, val, judgeRepo)
	if err != nil {
		return nil, err
	}
	return j.WithRepositories(repo), nil
}

func provideSchedulerLock(db *gorm.DB) port.SchedulerLock {
	return magi.NewSchedulerLock(db)
}

func provideToolQuotaRepository(db *gorm.DB) port.ToolQuotaRepository {
	return magi.NewToolQuotaRepository(db)
}

func provideToolQuotaService(cfg *Config, repo port.ToolQuotaRepository) *toolquota.Service {
	return toolquota.NewService(repo, cfg.ToolQuota.DefaultPerMinute, cfg.ToolQuota.Tools)
}

// usageBudgetChecker adapts admin usage aggregates to decision.BudgetChecker.
type usageBudgetChecker struct {
	admin   *admin.Service
	maxTok  int64
	maxCost float64
}

func (b *usageBudgetChecker) CheckBudget(ctx context.Context, userID int64) (*decision.BudgetExceededInfo, error) {
	if b == nil || b.admin == nil || (b.maxTok <= 0 && b.maxCost <= 0) {
		return &decision.BudgetExceededInfo{}, nil
	}
	budget, err := b.admin.Budget(ctx, userID, b.maxTok, b.maxCost)
	if err != nil {
		return nil, err
	}
	tokens, cost := budget.Exceeds()
	return &decision.BudgetExceededInfo{TokensExceeded: tokens, CostExceeded: cost}, nil
}

func provideBudgetChecker(adminSvc *admin.Service, cfg *Config) decision.BudgetChecker {
	if adminSvc == nil || (cfg.Limits.MaxTokensPerUser <= 0 && cfg.Limits.MaxCostUSDPerUser <= 0) {
		return nil
	}
	return &usageBudgetChecker{admin: adminSvc, maxTok: cfg.Limits.MaxTokensPerUser, maxCost: cfg.Limits.MaxCostUSDPerUser}
}

func provideRunManager(orch *orchestration.Orchestrator, repo port.Repository, jobs port.DecisionJobRepository, eventPub port.EventPublisher, reg *metrics.Registry, cfg *Config, budget decision.BudgetChecker) *decision.RunManager {
	live, _ := eventPub.(port.LiveEventPublisher)
	return decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), Metrics: reg,
		LiveEvents:               live,
		MaxConcurrentRunsPerUser: cfg.Limits.MaxConcurrentRunsPerUser,
		BudgetChecker:            budget,
	})
}

func provideDecisionService(
	orch *orchestration.Orchestrator,
	repo port.Repository,
	cfg *Config,
	rm *decision.RunManager,
	ttRepo port.TaskTreeRepository,
) *decision.Service {
	opts := []decision.Option{
		decision.WithCaseRepo(repo.CaseRepo()),
		decision.WithResolutionRepo(repo.ResolutionRepo()),
		decision.WithEvidenceRepo(repo.EvidenceRepo()),
		decision.WithClaimRepo(repo.ClaimRepo()),
		decision.WithVoteRepo(repo.VoteRepo()),
		decision.WithAgentRunRepo(repo.AgentRunRepo()),
		decision.WithToolCallRepo(repo.ToolCallRepo()),
		decision.WithRunManager(rm),
	}
	if tt, ok := ttRepo.(port.TaskTreeCleaner); ok {
		opts = append(opts, decision.WithTaskTreeCleaner(tt))
	}
	return decision.NewService(orch, decision.ServiceConfig{
		MaxDebateRounds: cfg.Magi.MaxDebateRounds,
	}, opts...)
}

func provideEventPublisher(repo port.Repository, broker *appserver.EventBroker, red *redact.Redactor) port.EventPublisher {
	return magi.NewEventPublisherAdapterWithRedaction(repo.EventRepo(), broker, red)
}

func provideContextBuilder(knowledge port.KnowledgePort, reg *metrics.Registry, eventPub port.EventPublisher) *domainmemory.ContextBuilder {
	return domainmemory.NewContextBuilder(knowledge,
		domainmemory.WithMetrics(reg),
		domainmemory.WithEventPublisher(eventPub),
	)
}
func provideEvaluationService(repo port.Repository) *evaluation.Service {
	return evaluation.NewService(evaluation.WithRepository(repo))
}

func provideReplayService(repo port.Repository) *replay.Service {
	return replay.NewService(repo.EventRepo())
}

func provideSelfImproveRepository(db *gorm.DB) port.SelfImproveRepository {
	return magi.NewSelfImproveRepository(db)
}

func provideSelfImproveService(repo port.Repository, sir port.SelfImproveRepository, prompts port.PromptRepository, modelPort *magi.ModelAdapter, cfg *Config) *selfimprove.Service {
	return selfimprove.NewService(sir, repo.CaseRepo(), repo.EventRepo(), repo.AgentRunRepo(),
		selfimprove.WithPrompts(prompts),
		selfimprove.WithAutoApply(cfg.SelfImprove.AutoApplyEnabled, cfg.SelfImprove.AutoApplyThreshold),
		selfimprove.WithMode(cfg.SelfImprove.Mode),
		selfimprove.WithModel(modelPort))
}

func provideSelfImproveHandler(svc *selfimprove.Service) *handler.SelfImproveHandler {
	return handler.NewSelfImproveHandler(svc)
}

func provideMemoryService(knowledge port.KnowledgePort, repo port.Repository, indexer port.MemoryIndexer) *memory.Service {
	return memory.NewService(knowledge, repo.MemoryRepo(), memory.WithCaseRepo(repo.CaseRepo()), memory.WithIndexer(indexer))
}

func provideRagIndexJobRepository(db *gorm.DB) port.RagIndexJobRepository {
	return magi.NewRagIndexJobRepository(db)
}

func provideRagIndexService(repo port.RagIndexJobRepository, agg port.Repository, knowRepo port.KnowledgeRepository, cfg *Config) *ragindex.Service {
	return ragindex.NewService(repo, agg.MemoryRepo(), knowRepo, cfg.RAG.IndexJobMaxAttempts)
}

func provideRagIndexPoller(repo port.RagIndexJobRepository, agg port.Repository, knowRepo port.KnowledgeRepository, kp port.KnowledgePort, cfg *Config) *ragindex.RagIndexPoller {
	// In async mode kp is *DurableIndexer; in sync mode it is the adapter
	// itself. The poller needs the concrete inner adapter to execute jobs.
	var inner *rag.HybridKnowledgeAdapter
	switch v := kp.(type) {
	case *rag.HybridKnowledgeAdapter:
		inner = v
	case *rag.DurableIndexer:
		inner = v.Inner()
	}
	host, _ := os.Hostname()
	return ragindex.NewRagIndexPoller(repo, agg.MemoryRepo(), knowRepo, inner, ragindex.PollerConfig{
		Interval:  time.Duration(cfg.RAG.IndexPollIntervalMS) * time.Millisecond,
		Lease:     time.Duration(cfg.RAG.IndexLeaseSeconds) * time.Second,
		RetryBase: time.Duration(cfg.RAG.IndexRetryBaseMS) * time.Millisecond,
		WorkerID:  "rag-index-" + host,
	})
}

func provideAdminRagHandler(svc *ragindex.Service) *handler.AdminRagHandler {
	return handler.NewAdminRagHandler(svc)
}

func provideKnowledgeRepository(db *gorm.DB) port.KnowledgeRepository {
	return magi.NewKnowledgeRepository(db)
}

func provideKnowledgeService(repo port.KnowledgeRepository, idx port.DocumentIndexer) *knowledge.Service {
	return knowledge.NewService(repo, idx)
}

func provideToolService(toolReg port.ToolRegistryPort) *tool.Service {
	return tool.NewService(toolReg)
}

func provideDatasetRepository(db *gorm.DB) port.DatasetRepository {
	return magi.NewDatasetRepository(db)
}

func provideDatasetService(datasets port.DatasetRepository, orch *orchestration.Orchestrator, repo port.Repository, cfg *Config, reg *metrics.Registry) *dataset.Service {
	return dataset.NewService(datasets, repo.CaseRepo(), orch, cfg.Magi.MaxDebateRounds,
		dataset.WithRunsPerItem(cfg.Benchmark.RunsPerItem),
		dataset.WithRegressionThreshold(cfg.Benchmark.RegressionThreshold),
		dataset.WithMetrics(reg))
}

// serverMaxRequestBodyBytes caps every Hertz handler before routing. The A2A
// surface additionally applies its own smaller per-request cap in transport.
const serverMaxRequestBodyBytes = 4 * 1024 * 1024

// serverListenAddr resolves the HTTP listen address from the environment.
// MAGI_HTTP_HOST sets the bind host (empty means all interfaces) and
// MAGI_HTTP_PORT sets the port (default 8080).
func serverListenAddr() string {
	host := os.Getenv("MAGI_HTTP_HOST")
	port := os.Getenv("MAGI_HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	return net.JoinHostPort(host, port)
}

func provideServer(lc fx.Lifecycle) *hzserver.Hertz {
	addr := serverListenAddr()
	h := hzserver.Default(
		hzserver.WithHostPorts(addr),
		hzserver.WithMaxRequestBodySize(serverMaxRequestBodyBytes),
		hzserver.WithSenseClientDisconnection(true),
	)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go h.Spin()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			// Graceful shutdown: stop accepting new connections,
			// drain in-flight requests + SSE streams, then exit.
			return h.Shutdown(ctx)
		},
	})
	return h
}

func registerLifecycle(lc fx.Lifecycle, rm *decision.RunManager, dsSvc *dataset.Service, siSvc *selfimprove.Service, poller *ragindex.RagIndexPoller, cfg *Config, a2a *A2A) {
	var autoCancel context.CancelFunc
	var ragCancel context.CancelFunc
	var a2aRecoveryCancel context.CancelFunc
	var decisionRecoveryCancel context.CancelFunc
	var decisionRecoveryDone chan struct{}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := rm.Recover(ctx); err != nil {
				// RecoverOnce may have launched workers before a repository
				// failure; the lifecycle failure path must not leak them.
				rm.Shutdown()
				return err
			}
			if err := a2a.Recover(ctx); err != nil {
				// Workers launched by rm.Recover above are derived from
				// context.Background(); a failed later startup step must stop
				// them before returning the error.
				rm.Shutdown()
				return err
			}
			if err := dsSvc.RecoverOrphanRuns(ctx); err != nil {
				rm.Shutdown()
				return err
			}
			// Every fallible startup step above has succeeded, so any goroutine
			// started below is owned by a hook that will run OnStop even when a
			// later step fails; a loop started before a later failure would leak.
			// Run continuous cross-replica recovery for expired decision-job
			// leases, owned by the lifecycle context so it stops on shutdown.
			decisionCtx, cancel := context.WithCancel(context.Background())
			decisionRecoveryCancel = cancel
			decisionRecoveryDone = make(chan struct{})
			go func() {
				defer close(decisionRecoveryDone)
				if err := rm.RunRecovery(decisionCtx); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("decision recovery stopped: %v", err)
				}
			}()
			if a2a.Enabled && a2a.SubmissionSvc != nil {
				// Continuous recovery for transiently failed A2A startup
				// leases, driven by the lifecycle context.
				a2aRecoveryCancel = a2a.StartRecoveryWorker(context.Background())
			}
			if poller != nil {
				ragCtx, cancel := context.WithCancel(context.Background())
				ragCancel = cancel
				go poller.Run(ragCtx)
			}
			if cfg.Benchmark.AutoIntervalSeconds > 0 {
				autoCtx, cancel := context.WithCancel(context.Background())
				autoCancel = cancel
				interval := time.Duration(cfg.Benchmark.AutoIntervalSeconds) * time.Second
				go func() {
					ticker := time.NewTicker(interval)
					defer ticker.Stop()
					for {
						select {
						case <-autoCtx.Done():
							return
						case <-ticker.C:
							if _, err := dsSvc.RunAutoRegression(autoCtx,
								cfg.Benchmark.AutoRunsPerItem, cfg.Benchmark.AutoRegressionThreshold); err != nil {
								if errors.Is(err, dataset.ErrRunActive) {
									continue // previous automated run still in flight
								}
								log.Printf("auto regression: %v", err)
							}
							if siSvc != nil && cfg.SelfImprove.AutoApplyEnabled {
								if applied, aerr := siSvc.AutoApply(autoCtx); aerr != nil {
									log.Printf("selfimprove auto-apply: %v", aerr)
								} else if applied > 0 {
									log.Printf("selfimprove auto-applied %d suggestion(s) after regression", applied)
								}
							}
						}
					}
				}()
			}
			return nil
		},
		OnStop: func(context.Context) error {
			if autoCancel != nil {
				autoCancel()
			}
			if ragCancel != nil {
				ragCancel()
			}
			if a2aRecoveryCancel != nil {
				a2aRecoveryCancel()
			}
			if decisionRecoveryCancel != nil {
				decisionRecoveryCancel()
				if decisionRecoveryDone != nil {
					<-decisionRecoveryDone
				}
			}
			// Stop any decision workers launched by recovery sweeps so a
			// graceful stop drains them too.
			rm.Shutdown()
			return nil
		},
	})
}

func provideDecisionJobRepository(db *gorm.DB) port.DecisionJobRepository {
	return magi.NewDecisionJobRepository(db)
}

// StubToolRegistry is a no-op tool registry for standalone mode.
type StubToolRegistry struct{}

func (s *StubToolRegistry) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	return nil, nil
}

// StubToolExecutor is a no-op tool executor for standalone mode.
type StubToolExecutor struct{}

func (s *StubToolExecutor) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	return &port.ToolExecutionResult{Output: "stub result"}, nil
}

func provideDB(cfg *Config) (*gorm.DB, error) {
	glog := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             slowThreshold(cfg.Database.SlowThresholdMs),
			LogLevel:                  gormLogLevel(cfg.Database.LogLevel),
			Colorful:                  false,
			IgnoreRecordNotFoundError: true,
		},
	)
	db, err := gorm.Open(MysqlDialector(cfg.Database.DSN), &gorm.Config{Logger: glog})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}
	// S16's event sequence schema must be applied by Atlas first; GORM
	// AutoMigrate must not attempt to alter the existing event table before its
	// nullable expand/backfill/contract sequence is complete.
	models := append(magi.AllModelsWithoutEventSequence(), rag.AllModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}
	if err := ensureEventSequenceSchema(db); err != nil {
		return nil, fmt.Errorf("failed to prepare event sequence schema: %w", err)
	}
	return db, nil
}

func ensureEventSequenceSchema(db *gorm.DB) error {
	hasEvents := db.Migrator().HasTable(&magi.EventModel{})
	hasCursor := db.Migrator().HasTable(&magi.EventCursorModel{})
	switch {
	case !hasEvents && !hasCursor:
		return db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{})
	case hasEvents && hasCursor:
		return verifyEventSequenceContract(db)
	default:
		return fmt.Errorf("partial event sequence schema detected; apply the complete Atlas S16 migration before starting the service")
	}
}

// verifyEventSequenceContract fails startup when both event tables exist but
// the S16 contract is only partially applied: a nullable seq, a missing unique
// (case_id, seq) index, or a stale cursor would let new writers persist broken
// sequences, so the service refuses to start with a migration-specific error.
func verifyEventSequenceContract(db *gorm.DB) error {
	notNull, err := columnNotNull(db, "magi_event", "seq")
	if err != nil {
		return fmt.Errorf("event sequence schema check failed: %w", err)
	}
	if !notNull {
		return fmt.Errorf("event sequence schema incomplete: magi_event.seq is nullable; complete S16 (backfill + NOT NULL) before starting and do not restart a pre-S16 writer")
	}
	unique, err := hasCaseSeqUniqueIndex(db, "magi_event")
	if err != nil {
		return fmt.Errorf("event sequence schema check failed: %w", err)
	}
	if !unique {
		return fmt.Errorf("event sequence schema incomplete: unique index (case_id, seq) is missing; apply the complete Atlas S16 migration")
	}
	return verifyEventCursor(db)
}

// hasCaseSeqUniqueIndex reports whether a UNIQUE index exists whose ordered
// columns are exactly [case_id, seq]. We do not trust names alone: a
// misleading same-name index on the wrong columns must not satisfy the
// contract.
func hasCaseSeqUniqueIndex(db *gorm.DB, table string) (bool, error) {
	indexes, err := uniqueIndexColumns(db, table)
	if err != nil {
		return false, err
	}
	for _, columns := range indexes {
		if len(columns) == 2 && columns[0] == "case_id" && columns[1] == "seq" {
			return true, nil
		}
	}
	return false, nil
}

// uniqueIndexColumns returns UNIQUE index names mapped to their ordered column
// names on the given table.
func uniqueIndexColumns(db *gorm.DB, table string) (map[string][]string, error) {
	out := map[string][]string{}
	if db.Dialector.Name() == "mysql" {
		type row struct {
			IndexName  string `gorm:"column:index_name"`
			NonUnique  int    `gorm:"column:non_unique"`
			SeqInIdx   int    `gorm:"column:seq_in_index"`
			ColumnName string `gorm:"column:column_name"`
		}
		var rows []row
		if err := db.Raw(`SELECT INDEX_NAME AS index_name, NON_UNIQUE AS non_unique,
			SEQ_IN_INDEX AS seq_in_index, COLUMN_NAME AS column_name
			FROM INFORMATION_SCHEMA.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
			ORDER BY INDEX_NAME, SEQ_IN_INDEX`, table).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.NonUnique == 0 {
				out[r.IndexName] = append(out[r.IndexName], r.ColumnName)
			}
		}
		return out, nil
	}
	// SQLite: index_list gives the unique-flag names; index_info gives the
	// ordered columns for a named index.
	rows, err := db.Raw("PRAGMA index_list(" + table + ")").Rows()
	if err != nil {
		return nil, err
	}
	type indexMeta struct {
		name string
		seq  int
	}
	var names []indexMeta
	for rows.Next() {
		var seq, unique int
		var name, origin, partial string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		if unique != 0 {
			names = append(names, indexMeta{name: name, seq: seq})
		}
	}
	rows.Close()
	for _, meta := range names {
		colRows, err := db.Raw("PRAGMA index_info(" + meta.name + ")").Rows()
		if err != nil {
			return nil, err
		}
		var cols []string
		for colRows.Next() {
			var seqno, cid int
			var cname string
			if err := colRows.Scan(&seqno, &cid, &cname); err != nil {
				colRows.Close()
				return nil, err
			}
			cols = append(cols, cname)
		}
		colRows.Close()
		out[meta.name] = cols
	}
	return out, nil
}

// columnNotNull reports whether a column is declared NOT NULL.
func columnNotNull(db *gorm.DB, table, column string) (bool, error) {
	if db.Dialector.Name() == "mysql" {
		var isNullable string
		if err := db.Raw("SELECT IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?", table, column).Row().Scan(&isNullable); err != nil {
			return false, err
		}
		return !strings.EqualFold(strings.TrimSpace(isNullable), "YES"), nil
	}
	rows, err := db.Raw("PRAGMA table_info(" + table + ")").Rows()
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return notNull != 0, nil
		}
	}
	return false, fmt.Errorf("column %s.%s not found", table, column)
}

// verifyEventCursor fails startup when an event-bearing case has no cursor or
// any cursor row is stale relative to the events it tracks (next_seq must equal
// MAX(seq)+1).
func verifyEventCursor(db *gorm.DB) error {
	var missing int64
	if err := db.Raw(`
		SELECT COUNT(DISTINCT e.case_id)
		FROM magi_event e
		LEFT JOIN magi_event_cursor c ON c.case_id = e.case_id
		WHERE c.case_id IS NULL`).
		Scan(&missing).Error; err != nil {
		return fmt.Errorf("event sequence schema check failed: %w", err)
	}
	if missing > 0 {
		return fmt.Errorf("event sequence schema incomplete: %d event-bearing cases are missing cursor rows; re-run the S16 backfill", missing)
	}
	var stale int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM magi_event_cursor c
		WHERE c.next_seq != COALESCE((SELECT MAX(e.seq) + 1 FROM magi_event e WHERE e.case_id = c.case_id), 1)`).
		Scan(&stale).Error; err != nil {
		return fmt.Errorf("event sequence schema check failed: %w", err)
	}
	if stale > 0 {
		return fmt.Errorf("event sequence schema incomplete: %d cursor rows are stale (next_seq != MAX(seq)+1); re-run the S16 backfill", stale)
	}
	return nil
}

// slowThreshold returns the configured GORM slow-query threshold, defaulting
// to 200ms when unset/invalid.
func slowThreshold(ms int) time.Duration {
	if ms <= 0 {
		return 200 * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// gormLogLevel maps a config string to a GORM logger level. Anything unknown
// defaults to Warn (errors + slow queries only, keeps runtime logs readable).
func gormLogLevel(s string) logger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}

func provideRepository(db *gorm.DB) port.Repository {
	return magi.NewRepository(db)
}

// provideSessionAuthorizer builds the store-backed revalidation used for OIDC
// session cookies. Without a user repository, sessions cannot be revalidated
// and are refused (fail closed).
func provideSessionAuthorizer(users port.UserRepository) *auth.SessionAuthorizer {
	if users == nil {
		return nil
	}
	return auth.NewSessionAuthorizer(users, auth.DefaultAuthStateTTL)
}

func provideAuthService(cfg *Config, users port.UserRepository, keys port.ApiKeyRepository, codec *auth.SessionCodec, authorizer *auth.SessionAuthorizer) *auth.Service {
	svc := auth.NewService(cfg.Auth.Enabled, staticKeySpecs(cfg)).WithStores(keys, users)
	if codec != nil {
		svc = svc.WithSession(codec)
		svc = svc.WithSessionAuthorizer(authorizer)
	}
	return svc
}

func staticKeySpecs(cfg *Config) []auth.KeySpec {
	keys := make([]auth.KeySpec, 0, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		keys = append(keys, auth.KeySpec{Name: k.Name, Key: k.Key, KeyHash: k.KeyHash, UserID: k.UserID, Role: k.Role})
	}
	return keys
}

func provideUserRepository(db *gorm.DB) port.UserRepository {
	return magi.NewUserRepository(db)
}

func provideApiKeyRepository(db *gorm.DB) port.ApiKeyRepository {
	return magi.NewApiKeyRepository(db)
}

func provideUsersService(userRepo port.UserRepository, keyRepo port.ApiKeyRepository, cfg *Config, authorizer *auth.SessionAuthorizer) *users.Service {
	selfRegistration := cfg.Auth.SelfRegistration || cfg.Auth.OIDC.SelfRegistration
	opts := []func(*users.Service){users.WithSelfRegistration(selfRegistration)}
	if authorizer != nil {
		opts = append(opts, users.WithSessionInvalidator(authorizer))
	}
	return users.NewServiceWithOptions(userRepo, keyRepo, opts...)
}

func provideSessionCodec(cfg *Config) (*auth.SessionCodec, error) {
	if !cfg.Auth.OIDC.Enabled || strings.TrimSpace(cfg.Auth.OIDC.SessionSecret) == "" {
		return nil, nil
	}
	ttl := time.Duration(cfg.Auth.OIDC.SessionTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return auth.NewSessionCodec(cfg.Auth.OIDC.SessionSecret, ttl)
}

func provideOIDCClient(cfg *Config, users port.UserRepository) (*auth.OIDCClient, error) {
	if !cfg.Auth.OIDC.Enabled {
		return nil, nil
	}
	return auth.NewOIDCClient(auth.OIDCConfig{
		Enabled: cfg.Auth.OIDC.Enabled, Issuer: cfg.Auth.OIDC.Issuer,
		ClientID: cfg.Auth.OIDC.ClientID, ClientSecret: cfg.Auth.OIDC.ClientSecret,
		RedirectURL: cfg.Auth.OIDC.RedirectURL, Scopes: cfg.Auth.OIDC.Scopes,
		SelfRegistration: cfg.Auth.OIDC.SelfRegistration,
	}, users)
}

func provideOIDCHandler(client *auth.OIDCClient, codec *auth.SessionCodec, usersSvc *users.Service, auditSvc *audit.Service) *handler.OIDCHandler {
	return handler.NewOIDCHandler(client, codec, usersSvc, auditSvc)
}

func providePluginBindingRepository(db *gorm.DB) port.PluginBindingRepository {
	return magi.NewPluginBindingRepository(db)
}

func providePluginsService(repo port.PluginBindingRepository) *plugins.Service {
	return plugins.NewService(repo)
}

func codeRunnerAdapter(cfg *Config) port.CodeRunnerPort {
	enabled := true
	if cfg.CodeRunner.Enabled != nil {
		enabled = *cfg.CodeRunner.Enabled
	}
	if !enabled {
		return nil
	}
	p := magi.DefaultCodeRunnerPolicy()
	if cfg.CodeRunner.TimeoutSeconds > 0 {
		p.TimeoutSeconds = cfg.CodeRunner.TimeoutSeconds
	}
	if cfg.CodeRunner.MaxCodeChars > 0 {
		p.MaxCodeChars = cfg.CodeRunner.MaxCodeChars
	}
	if len(cfg.CodeRunner.AllowedLanguages) > 0 {
		p.AllowedLanguages = cfg.CodeRunner.AllowedLanguages
	}
	if len(cfg.CodeRunner.BlockedPatterns) > 0 {
		p.BlockedPatterns = cfg.CodeRunner.BlockedPatterns
	}
	if cfg.CodeRunner.Docker.Enabled {
		docker, err := magi.NewDockerCodeRunnerAdapter(magi.DockerCodeRunnerPolicy{
			CodeRunnerPolicy: p,
			Image:            cfg.CodeRunner.Docker.Image,
			MemoryMB:         cfg.CodeRunner.Docker.MemoryMB,
			CPUs:             cfg.CodeRunner.Docker.CPUs,
			Runtime:          cfg.CodeRunner.Docker.Runtime,
			DockerTimeout:    cfg.CodeRunner.Docker.TimeoutSeconds,
			DefaultTimeout:   p.TimeoutSeconds,
		}, nil)
		if err != nil {
			log.Printf("code_runner docker: %v", err)
			return nil
		}
		return docker
	}
	sr := coderunnersandbox.NewRunner(&coderunnersandbox.Config{
		AllowEnv:       cfg.CodeRunner.AllowEnv,
		AllowRead:      cfg.CodeRunner.AllowRead,
		AllowWrite:     cfg.CodeRunner.AllowWrite,
		AllowNet:       cfg.CodeRunner.AllowNet,
		AllowRun:       cfg.CodeRunner.AllowRun,
		AllowFFI:       cfg.CodeRunner.AllowFFI,
		NodeModulesDir: cfg.CodeRunner.NodeModulesDir,
		TimeoutSeconds: float64(p.TimeoutSeconds),
		MemoryLimitMB:  cfg.CodeRunner.MemoryLimitMB,
	})
	return magi.NewCodeRunnerAdapterWithRunner(sr, p)
}

func provideAdminService(repo port.Repository) *admin.Service {
	return admin.NewService(repo.CaseRepo(), repo.AgentRunRepo())
}

func provideToolPolicy(cfg *Config) *toolpolicy.Policy {
	return toolpolicy.NewPolicy(cfg.ToolPolicy.RequireApproval, cfg.ToolPolicy.AutoApproved)
}

func provideRecurringRepository(db *gorm.DB) port.RecurringRepository {
	return magi.NewRecurringRepository(db)
}

func provideRecurringService(repo port.RecurringRepository, agg port.Repository, rm *decision.RunManager, cfg *Config) *recurring.Service {
	return recurring.NewService(repo, agg.CaseRepo(), rm, cfg.Magi.MaxDebateRounds)
}

func registerScheduler(lc fx.Lifecycle, svc *recurring.Service, lock port.SchedulerLock) {
	ctx, cancel := context.WithCancel(context.Background())
	owner := "scheduler-" + uuid.NewString()
	lc.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			go recurring.NewSchedulerWithLock(svc, time.Minute, lock, owner).Run(ctx)
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			return nil
		},
	})
}

func provideConversationRepository(db *gorm.DB) port.ConversationRepository {
	return magi.NewConversationRepository(db)
}

func provideAssistantService(decSvc *decision.Service, convRepo port.ConversationRepository) *assistant.Service {
	return assistant.NewService(decSvc, assistant.WithConversationRepository(convRepo))
}

func provideHealthPinger(db *gorm.DB) func(context.Context) error {
	return func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}
}

func provideRedactor(cfg *Config) *redact.Redactor {
	secrets := []string{cfg.Model.APIKey, cfg.Tavily.APIKey}
	for _, provider := range webSearchProviderSpecs(cfg) {
		secrets = append(secrets, provider.APIKey)
	}
	for _, k := range cfg.Auth.APIKeys {
		secrets = append(secrets, k.Key)
	}
	return redact.New(secrets...)
}

// A2A is an optional bundle of A2A server components. A disabled config still
// yields a non-nil wrapper so Fx consumers never receive nil interfaces.
type A2A struct {
	Enabled         bool
	SubmissionRepo  a2aapp.SubmissionRepository
	SubmissionSvc   *a2aapp.SubmissionService
	TaskProjector   *a2aapp.TaskProjector
	StreamProjector a2aapp.StreamProjector
	Handler         a2asrv.RequestHandler
	MountDeps       *a2atransport.MountDeps
}

// Recover replays the A2A start classifier for PREPARED bindings. It is a
// no-op when the feature is disabled.
func (a *A2A) Recover(ctx context.Context) error {
	if a == nil || !a.Enabled || a.SubmissionSvc == nil {
		return nil
	}
	return a.SubmissionSvc.Recover(ctx)
}

// StartRecoveryWorker launches the continuous recovery loop and returns a
// cancel function that stops it. It is a no-op when the feature is disabled.
func (a *A2A) StartRecoveryWorker(parent context.Context) context.CancelFunc {
	if a == nil || !a.Enabled || a.SubmissionSvc == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	go func() {
		if err := a.SubmissionSvc.RunRecovery(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("a2a recovery worker stopped: %v", err)
		}
	}()
	return cancel
}

// ProvideA2A wires the optional A2A server from config and existing
// repositories. When a2a.enabled is false it returns a disabled wrapper that
// registers no lifecycle work and no routes.
func ProvideA2A(db *gorm.DB, cfg *Config, rm *decision.RunManager, broker *appserver.EventBroker, repo port.Repository, reg *metrics.Registry, red *redact.Redactor, auditSvc *audit.Service) *A2A {
	if cfg == nil || !cfg.A2A.Enabled {
		return &A2A{Enabled: false}
	}
	a2aRepo := magi.NewA2ASubmissionRepository(db)
	parser := a2aapp.NewInputParser(cfg.A2A.MaxMessageBytes, cfg.A2A.MaxParts)
	cursor := a2aapp.CursorCodec{MaxPageSize: cfg.A2A.MaxPageSize}
	proj := a2aapp.NewTaskProjector(red)
	svc := a2aapp.NewSubmissionService(parser, a2aRepo, rm, proj, cfg.Magi.MaxDebateRounds,
		a2aapp.WithSubmissionMetrics(reg))
	stream := a2aapp.NewDurableStreamProjector(a2aRepo, repo.EventRepo(), broker, proj,
		cfg.A2A.MaxStreamsPerUserPerReplica, cfg.A2A.CrossInstancePollInterval,
		a2aapp.WithStreamMetrics(reg))
	handler := a2aapp.NewHandler(svc, a2aRepo, proj, cursor, rm, stream,
		a2aapp.WithHandlerMetrics(reg), a2aapp.WithHandlerAudit(auditSvc),
		a2aapp.WithHandlerLivePublisher(broker))
	return &A2A{
		Enabled:         true,
		SubmissionRepo:  a2aRepo,
		SubmissionSvc:   svc,
		TaskProjector:   proj,
		StreamProjector: stream,
		Handler:         handler,
		MountDeps: &a2atransport.MountDeps{
			Handler: handler, PublicURL: cfg.A2A.PublicURL, BasePath: cfg.A2A.BasePath,
			Name: "MAGI", Description: "Evidence-driven decision assistant",
			MaxRequestBytes: int64(cfg.A2A.MaxRequestBytes),
		},
	}
}

func provideTracingProvider(cfg *Config) *trace.TracerProvider {
	return tracing.NewProvider(tracing.Config{Enabled: cfg.Tracing.Enabled, ServiceName: cfg.Tracing.ServiceName, OTLPEndpoint: cfg.Tracing.OTLPEndpoint}, nil)
}

func registerTracingShutdown(lc fx.Lifecycle, tp *trace.TracerProvider) {
	if tp == nil {
		return
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		},
	})
}

// providePromptRepository builds the DB-backed prompt store and seeds the
// built-in templates when the table is empty (P2 D12).
func providePromptRepository(db *gorm.DB) (port.PromptRepository, error) {
	repo := magi.NewPromptRepository(db)
	if err := seedPrompts(context.Background(), repo); err != nil {
		return nil, err
	}
	return repo, nil
}

func provideRolePolicyRepository(db *gorm.DB) port.RolePolicyRepository {
	return magi.NewRolePolicyRepository(db)
}

func provideRolePolicyService(repo port.RolePolicyRepository) *rolepolicy.Service {
	return rolepolicy.NewService(repo)
}

func provideRolePolicyHandler(svc *rolepolicy.Service) *handler.RolePolicyHandler {
	return handler.NewRolePolicyHandler(svc)
}

func provideGoldenRepository(db *gorm.DB) port.GoldenRepository {
	return magi.NewGoldenRepository(db)
}

func provideGoldenService(repo port.Repository, goldenRepo port.GoldenRepository, datasets port.DatasetRepository) *golden.Service {
	return golden.NewService(goldenRepo, repo.CaseRepo(), repo.ResolutionRepo(), datasets)
}

func provideGoldenHandler(svc *golden.Service) *handler.GoldenHandler {
	return handler.NewGoldenHandler(svc)
}

func seedPrompts(ctx context.Context, repo port.PromptRepository) error {
	existing, err := repo.List(ctx)
	if err != nil {
		return fmt.Errorf("prompts: list: %w", err)
	}
	have := map[string]bool{}
	for _, t := range existing {
		if t != nil && t.Active {
			have[t.Key] = true
		}
	}
	defaults := promptpkg.Default()
	for key, content := range defaults {
		if have[key] {
			continue
		}
		if _, err := repo.Restore(ctx, key, content); err != nil {
			return fmt.Errorf("prompts: seed %s: %w", key, err)
		}
	}
	return nil
}

func providePromptProvider(repo port.PromptRepository) port.PromptProvider {
	return magi.NewDBPromptProvider(repo)
}
