package bootstrap

import (
	"context"
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
	"github.com/jamespud/magi/backend/application/judge"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/plugins"
	"github.com/jamespud/magi/backend/application/recurring"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/application/toolpolicy"
	"github.com/jamespud/magi/backend/application/toolquota"
	"github.com/jamespud/magi/backend/application/tracing"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
	domainmemory "github.com/jamespud/magi/backend/domain/memory"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/runtime"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
	appserver "github.com/jamespud/magi/backend/server"
)

// Module is the Uber Fx module that wires all MAGI dependencies.
var Module = fx.Options(
	fx.Provide(
		// Validation
		validation.NewReflectSchemaGenerator,
		validation.NewJSONSchemaValidator,

		// Adapters (standalone mode; Coze mode replaces these)
		magi.NewModelAdapter,
		appserver.NewEventBroker,
		provideEventPublisher,
		provideContextBuilder,
		ProvideToolRegistry,
		ProvideToolExecutor,
		provideMCPAdapter,
		provideAuthService,
		ProvideKnowledgePort,

		// Database
		provideDB,
		provideRepository,
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

		// Agent runtime
		provideAgentLoop,
		provideCommander,
		provideMagiConfigs,
		provideOrchestrator,

		// Application
		provideRunManager,
		provideDecisionService,
		provideReplayService,
		provideEvaluationService,
		provideMemoryService,
		provideToolService,
		provideDatasetService,
		providePluginsService,
		provideAdminService,
		provideRecurringRepository,
		provideRecurringService,
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
		evalSvc *evaluation.Service,
		memSvc *memory.Service,
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
			Evaluation:   evalSvc,
			Memory:       memSvc,
			Tool:         toolSvc,
			Broker:       broker,
			EventRepo:    repo.EventRepo(),
			HealthPinger: dbPing,
			Tracing:      tp,
			ModelName:    cfg.Model.ModelName,
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
) (*runtime.AgentLoop, error) {
	adapterRegistry := evidence.NewEvidenceAdapterRegistry(
		evidence.FullReliabilityResolver(),
		evidence.NewTavilyAdapter(),
		evidence.NewNativeAdapter(),
		evidence.NewRawObservationAdapter(),
	)
	return runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelPort, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen, EventPub: eventPub, CheckpointRepo: repo.CheckpointRepo(), Adapter: adapterRegistry, ToolPolicy: toolPol, Metrics: reg, Redactor: red, ApprovalRepo: approvalRepo, Quota: quota,
	})
}

// ProvideToolRegistry routes local/plugin/workflow/code-runner/MCP bindings through one registry.
func ProvideToolRegistry(cfg *Config, mcpAdapter *mcpadapter.Adapter) port.ToolRegistryPort {
	var local port.ToolRegistryPort
	if cfg.Tavily.APIKey != "" {
		local = magi.NewLocalToolRegistry()
	}
	var mcpReg port.ToolRegistryPort
	if len(cfg.MCP.Servers) > 0 {
		mcpReg = mcpAdapter
	}
	return magi.NewToolRegistryMuxWithAll(local, magi.NewPluginAdapter(crossplugin.DefaultSVC()),
		magi.NewWorkflowAdapter(crossworkflow.DefaultSVC()), codeRunnerAdapter(cfg), mcpReg)
}

// ProvideToolExecutor routes local/plugin/workflow/code-runner/MCP execution through one executor.
func ProvideToolExecutor(cfg *Config, mcpAdapter *mcpadapter.Adapter) port.ToolExecutorPort {
	var local port.ToolExecutorPort
	if cfg.Tavily.APIKey != "" {
		local = magi.NewTavilyToolExecutor(cfg.Tavily.APIKey)
	}
	var mcpExec port.ToolExecutorPort
	if len(cfg.MCP.Servers) > 0 {
		mcpExec = mcpAdapter
	}
	return magi.NewToolExecutorMuxWithAll(local, magi.NewPluginAdapter(crossplugin.DefaultSVC()),
		magi.NewWorkflowAdapter(crossworkflow.DefaultSVC()), codeRunnerAdapter(cfg), mcpExec)
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
func ProvideKnowledgePort(cfg *Config, db *gorm.DB, pub port.EventPublisher) (port.KnowledgePort, error) {
	ch := rag.NewChunker(rag.RuneTokenCounter{CharsPerToken: 4}, rag.ChunkLevels{L1800: 1800, L900: 900, L300: 300})
	emb := rag.NewOpenAIEmbedder(cfg.Embedding.BaseURL, cfg.Embedding.APIKey, cfg.Embedding.ModelName, cfg.Embedding.Dim)

	var vec rag.VectorIndex
	if cfg.Milvus.Address != "" {
		real, err := rag.NewMilvusIndexer(cfg.Milvus.Address, cfg.Milvus.Collection, cfg.Embedding.Dim)
		if err != nil {
			return nil, fmt.Errorf("milvus connect %q: %w (set milvus.address empty to use fake index)", cfg.Milvus.Address, err)
		}
		vec = real
	} else {
		vec = &rag.FakeVectorIndex{}
	}
	var lex rag.LexicalIndex
	if len(cfg.Elasticsearch.Addresses) > 0 {
		real, err := rag.NewESIndexer(cfg.Elasticsearch.Addresses, cfg.Elasticsearch.Index)
		if err != nil {
			return nil, fmt.Errorf("elasticsearch connect %v: %w (set elasticsearch.addresses empty to use fake index)", cfg.Elasticsearch.Addresses, err)
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
		adapter = rag.NewHybridKnowledgeAdapter(ch, emb, repo, vec, lex, retriever, nil)
		return rag.NewAsyncIndexer(adapter, pub, cfg.RAG.StoreWorkers), nil
	}
	return adapter, nil
}

func provideCommander(
	cfg *Config,
	modelPort *magi.ModelAdapter,
	gen validation.SchemaGenerator,
	val validation.Validator,
) (*service.Commander, error) {
	return service.NewCommander(
		service.CommanderConfig{
			Model:   entity.ModelRef{APIKey: cfg.Model.APIKey, BaseURL: cfg.Model.BaseURL, ModelName: cfg.Model.ModelName},
			Persona: "commander",
		},
		modelPort, gen, val,
	)
}

func provideMagiConfigs(cfg *Config) []*entity.MagiConfig {
	return []*entity.MagiConfig{
		cfg.Magi.Melchior.ToConfig("melchior", cfg),
		cfg.Magi.Balthasar.ToConfig("balthasar", cfg),
		cfg.Magi.Casper.ToConfig("casper", cfg),
	}
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
	j, err := judge.NewService(modelPort, entity.ModelRef{APIKey: cfg.Model.APIKey, BaseURL: cfg.Model.BaseURL, ModelName: cfg.Model.ModelName}, gen, val, judgeRepo)
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

func provideRunManager(orch *orchestration.Orchestrator, repo port.Repository, jobs port.DecisionJobRepository, reg *metrics.Registry, cfg *Config) *decision.RunManager {
	return decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), Metrics: reg,
		MaxConcurrentRunsPerUser: cfg.Limits.MaxConcurrentRunsPerUser,
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

func provideContextBuilder(knowledge port.KnowledgePort) *domainmemory.ContextBuilder {
	return domainmemory.NewContextBuilder(knowledge)
}
func provideEvaluationService(repo port.Repository) *evaluation.Service {
	return evaluation.NewService(evaluation.WithRepository(repo))
}

func provideReplayService(repo port.Repository) *replay.Service {
	return replay.NewService(repo.EventRepo())
}

func provideMemoryService(knowledge port.KnowledgePort, repo port.Repository) *memory.Service {
	return memory.NewService(knowledge, repo.MemoryRepo(), memory.WithCaseRepo(repo.CaseRepo()))
}

func provideToolService(toolReg port.ToolRegistryPort) *tool.Service {
	return tool.NewService(toolReg)
}

func provideDatasetRepository(db *gorm.DB) port.DatasetRepository {
	return magi.NewDatasetRepository(db)
}

func provideDatasetService(datasets port.DatasetRepository, orch *orchestration.Orchestrator, repo port.Repository, cfg *Config) *dataset.Service {
	return dataset.NewService(datasets, repo.CaseRepo(), orch, cfg.Magi.MaxDebateRounds,
		dataset.WithRunsPerItem(cfg.Benchmark.RunsPerItem), dataset.WithRegressionThreshold(cfg.Benchmark.RegressionThreshold))
}

func provideServer(lc fx.Lifecycle) *hzserver.Hertz {
	h := hzserver.Default(hzserver.WithHostPorts(":8080"))
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

func registerLifecycle(lc fx.Lifecycle, rm *decision.RunManager, dsSvc *dataset.Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := rm.Recover(ctx); err != nil {
				return err
			}
			return dsSvc.RecoverOrphanRuns(ctx)
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

func provideAuthService(cfg *Config) *auth.Service {
	keys := make([]auth.KeySpec, 0, len(cfg.Auth.APIKeys))
	for _, k := range cfg.Auth.APIKeys {
		keys = append(keys, auth.KeySpec{Name: k.Name, Key: k.Key, KeyHash: k.KeyHash, UserID: k.UserID, Role: k.Role})
	}
	return auth.NewService(cfg.Auth.Enabled, keys)
}

func providePluginBindingRepository(db *gorm.DB) port.PluginBindingRepository {
	return magi.NewPluginBindingRepository(db)
}

func providePluginsService(repo port.PluginBindingRepository) *plugins.Service {
	return plugins.NewService(repo)
}

func codeRunnerAdapter(cfg *Config) *magi.CodeRunnerAdapter {
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

func provideAssistantService(decSvc *decision.Service) *assistant.Service {
	return assistant.NewService(decSvc)
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
