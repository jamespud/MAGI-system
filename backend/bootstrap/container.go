package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log"
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

	magi "github.com/jamespud/magi/backend/adapter"
	mcpadapter "github.com/jamespud/magi/backend/adapter/mcp"
	rag "github.com/jamespud/magi/backend/adapter/rag"
	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/approval"
	"github.com/jamespud/magi/backend/application/assistant"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/golden"
	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/application/knowledge"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
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
	domainmemory "github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	promptpkg "github.com/jamespud/magi/backend/domain/prompt"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
	appserver "github.com/jamespud/magi/backend/server"
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
		provideRunCounter,
		provideBudgetChecker,

		// Agent runtime
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
		provideMemoryIndexer,
		provideMemoryService,
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
		dbPing func(context.Context) error,
		tp *trace.TracerProvider,
	) {
		appserver.RegisterRoutesWithDeps(h, appserver.RouteDeps{
			Decision:     decSvc,
			Approval:     apprSvc,
			Judge:        judgeSvc,
			Auth:         authSvc,
			Metrics:      reg,
			Dataset:      dsSvc,
			Plugins:      plugs,
			Admin:        admSvc,
			Recurring:    recSvc,
			Assistant:    askSvc,
			Replay:       repSvc,
			SelfImprove:  siH,
			RolePolicy:   rpH,
			Golden:       goldenH,
			Evaluation:   evalSvc,
			Memory:       memSvc,
			Knowledge:    knowSvc,
			Users:        usersSvc,
			OIDC:         oidcH,
			Tool:         toolSvc,
			Broker:       broker,
			EventRepo:    repo.EventRepo(),
			HealthPinger: dbPing,
			Tracing:      tp,
			ModelName:    cfg.Model.ModelName,
			MaxSteps:     cfg.Magi.MaxSteps,
			Export:       handler.NewExportHandler(decSvc, repo.EventRepo(), memSvc, evalSvc, judgeSvc),
			RateLimit: appserver.RateLimitConfig{
				Enabled:          cfg.HTTPRateLimit.Enabled,
				PerUserPerMinute: cfg.HTTPRateLimit.PerUserPerMinute,
				PerIPPerMinute:   cfg.HTTPRateLimit.PerIPPerMinute,
			},
			MetricsAuth:       cfg.Metrics.AuthRequired,
			MaxTokensPerUser:  cfg.Limits.MaxTokensPerUser,
			MaxCostUSDPerUser: cfg.Limits.MaxCostUSDPerUser,
			PromptRepo:        repo.PromptRepo(),
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
	prompts port.PromptProvider,
) (*runtime.AgentLoop, error) {
	adapterRegistry := evidence.NewEvidenceAdapterRegistry(
		evidence.FullReliabilityResolver(),
		evidence.NewWebSearchAdapter(),
		evidence.NewNativeAdapter(),
		evidence.NewRawObservationAdapter(),
	)
	return runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelPort, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen, EventPub: eventPub, CheckpointRepo: repo.CheckpointRepo(), Adapter: adapterRegistry, ToolPolicy: toolPol, Metrics: reg, Redactor: red, ApprovalRepo: approvalRepo, Quota: quota, Prompts: prompts,
	})
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
	loop *runtime.AgentLoop, magiConfigs []*entity.MagiConfig) (port.ToolExecutorPort, error) {
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
		investigator, err := magi.NewLoopSubInvestigator(loop, roleCfg)
		if err != nil {
			return nil, err
		}
		delegate, err := magi.NewDelegateToolExecutor(investigator)
		if err != nil {
			return nil, err
		}
		executors[magi.DelegateToolName] = delegate
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
func ProvideKnowledgePort(cfg *Config, db *gorm.DB, pub port.EventPublisher) (port.KnowledgePort, port.DocumentIndexer, error) {
	ch := rag.NewChunker(rag.RuneTokenCounter{CharsPerToken: 4}, rag.ChunkLevels{L1800: 1800, L900: 900, L300: 300})
	emb := rag.NewOpenAIEmbedder(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.ModelName, cfg.Embedding.Dim)

	var vec rag.VectorIndex
	if cfg.Milvus.Address != "" {
		real, err := rag.NewMilvusIndexer(cfg.Milvus.Address, cfg.Milvus.Collection, cfg.Embedding.Dim)
		if err != nil {
			return nil, nil, fmt.Errorf("milvus connect %q: %w (set milvus.address empty to use fake index)", cfg.Milvus.Address, err)
		}
		vec = real
	} else {
		vec = &rag.FakeVectorIndex{}
	}
	var lex rag.LexicalIndex
	if len(cfg.Elasticsearch.Addresses) > 0 {
		real, err := rag.NewESIndexer(cfg.Elasticsearch.Addresses, cfg.Elasticsearch.Index)
		if err != nil {
			return nil, nil, fmt.Errorf("elasticsearch connect %v: %w (set elasticsearch.addresses empty to use fake index)", cfg.Elasticsearch.Addresses, err)
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
	adapter := rag.NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever, pub)
	if cfg.RAG.StoreAsync {
		// Async path: the inner adapter must not publish MEMORY_INDEXED
		// itself; the worker publishes once indexing actually completes.
		inner := rag.NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever, nil)
		return rag.NewAsyncIndexer(inner, pub, cfg.RAG.StoreWorkers), adapter, nil
	}
	return adapter, adapter, nil
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
) *orchestration.Orchestrator {
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
		Policy:               consensus.DefaultConsensusPolicy(),
		ToolBindingsProvider: plugs,
	})
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

func provideRunCounter(db *gorm.DB) port.RunCounter {
	return magi.NewRunCounterRepository(db)
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

func provideRunManager(orch *orchestration.Orchestrator, repo port.Repository, jobs port.DecisionJobRepository, reg *metrics.Registry, cfg *Config, counter port.RunCounter, budget decision.BudgetChecker) *decision.RunManager {
	return decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), Metrics: reg,
		MaxConcurrentRunsPerUser: cfg.Limits.MaxConcurrentRunsPerUser,
		RunCounter:               counter,
		BudgetChecker:            budget,
	})
}

func provideDecisionService(
	orch *orchestration.Orchestrator,
	repo port.Repository,
	cfg *Config,
	rm *decision.RunManager,
) *decision.Service {
	return decision.NewService(orch, decision.ServiceConfig{
		MaxDebateRounds: cfg.Magi.MaxDebateRounds,
	}, decision.WithCaseRepo(repo.CaseRepo()),
		decision.WithResolutionRepo(repo.ResolutionRepo()),
		decision.WithEvidenceRepo(repo.EvidenceRepo()),
		decision.WithClaimRepo(repo.ClaimRepo()),
		decision.WithVoteRepo(repo.VoteRepo()),
		decision.WithAgentRunRepo(repo.AgentRunRepo()),
		decision.WithToolCallRepo(repo.ToolCallRepo()),
		decision.WithRunManager(rm))
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

func provideSelfImproveService(repo port.Repository, sir port.SelfImproveRepository, prompts port.PromptRepository, cfg *Config) *selfimprove.Service {
	return selfimprove.NewService(sir, repo.CaseRepo(), repo.EventRepo(), repo.AgentRunRepo(),
		selfimprove.WithPrompts(prompts),
		selfimprove.WithAutoApply(cfg.SelfImprove.AutoApplyEnabled, cfg.SelfImprove.AutoApplyThreshold))
}

func provideSelfImproveHandler(svc *selfimprove.Service) *handler.SelfImproveHandler {
	return handler.NewSelfImproveHandler(svc)
}

func provideMemoryIndexer(knowledge port.KnowledgePort) port.MemoryIndexer {
	if indexer, ok := knowledge.(port.MemoryIndexer); ok {
		return indexer
	}
	return nil
}

func provideMemoryService(knowledge port.KnowledgePort, repo port.Repository, indexer port.MemoryIndexer) *memory.Service {
	return memory.NewService(knowledge, repo.MemoryRepo(), memory.WithCaseRepo(repo.CaseRepo()), memory.WithIndexer(indexer))
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

func provideServer(lc fx.Lifecycle) *hzserver.Hertz {
	addr := os.Getenv("MAGI_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	h := hzserver.Default(hzserver.WithHostPorts(addr))
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

func registerLifecycle(lc fx.Lifecycle, rm *decision.RunManager, dsSvc *dataset.Service, siSvc *selfimprove.Service, cfg *Config) {
	var autoCancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := rm.Recover(ctx); err != nil {
				return err
			}
			if err := dsSvc.RecoverOrphanRuns(ctx); err != nil {
				return err
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
	models := append(magi.AllModels(), rag.AllModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}
	return db, nil
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

func provideAuthService(cfg *Config, users port.UserRepository, keys port.ApiKeyRepository, codec *auth.SessionCodec) *auth.Service {
	svc := auth.NewService(cfg.Auth.Enabled, staticKeySpecs(cfg)).WithStores(keys, users)
	if codec != nil {
		svc = svc.WithSession(codec)
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

func provideUsersService(userRepo port.UserRepository, keyRepo port.ApiKeyRepository, cfg *Config) *users.Service {
	selfRegistration := cfg.Auth.SelfRegistration || cfg.Auth.OIDC.SelfRegistration
	return users.NewServiceWithOptions(userRepo, keyRepo, users.WithSelfRegistration(selfRegistration))
}

func provideSessionCodec(cfg *Config) *auth.SessionCodec {
	if !cfg.Auth.OIDC.Enabled || strings.TrimSpace(cfg.Auth.OIDC.SessionSecret) == "" {
		return nil
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

func provideOIDCHandler(client *auth.OIDCClient, codec *auth.SessionCodec, usersSvc *users.Service) *handler.OIDCHandler {
	return handler.NewOIDCHandler(client, codec, usersSvc)
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
