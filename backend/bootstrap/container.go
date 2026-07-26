package bootstrap

import (
	"context"
	"fmt"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/fx"
	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
	rag "github.com/jamespud/magi/backend/adapter/rag"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/application/memory"
	"github.com/jamespud/magi/backend/application/replay"
	"github.com/jamespud/magi/backend/application/tool"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/evidence"
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
		ProvideToolRegistry,
		ProvideToolExecutor,

		// Database
		provideDB,
		provideRepository,

		// Agent runtime
		provideAgentLoop,
		provideCommander,
		provideMagiConfigs,
		provideOrchestrator,

		// Application
		provideRunManager,
		provideDecisionService,
		provideReplayService,
		evaluation.NewService,
		provideMemoryService,
		provideToolService,

		// Server
		provideServer,
	),
	fx.Invoke(func(
		h *hzserver.Hertz,
		decSvc *decision.Service,
		repSvc *replay.Service,
		evalSvc *evaluation.Service,
		memSvc *memory.Service,
		toolSvc *tool.Service,
		broker *appserver.EventBroker,
	) {
		appserver.RegisterRoutesWithDeps(h, appserver.RouteDeps{
			Decision:   decSvc,
			Replay:     repSvc,
			Evaluation: evalSvc,
			Memory:     memSvc,
			Tool:       toolSvc,
			Broker:     broker,
		})
	}),
	fx.Invoke(registerLifecycle),
)

func provideAgentLoop(
	modelPort *magi.ModelAdapter,
	toolReg port.ToolRegistryPort,
	toolExec port.ToolExecutorPort,
	val validation.Validator,
	gen validation.SchemaGenerator,
	broker *appserver.EventBroker,
) (*runtime.AgentLoop, error) {
	adapterRegistry := evidence.NewEvidenceAdapterRegistry(
		evidence.FullReliabilityResolver(),
		evidence.NewTavilyAdapter(),
		evidence.NewNativeAdapter(),
		evidence.NewRawObservationAdapter(),
	)
	return runtime.NewAgentLoop(runtime.AgentLoopDeps{
		ModelPort: modelPort, ToolReg: toolReg, ToolExec: toolExec,
		Validator: val, Gen: gen, EventPub: broker, Adapter: adapterRegistry,
	})
}

// ProvideToolRegistry returns a LocalToolRegistry (resolves web_search) when a
// Tavily key is configured, else the no-op StubToolRegistry (no tools).
func ProvideToolRegistry(cfg *Config) port.ToolRegistryPort {
	if cfg.Tavily.APIKey != "" {
		return magi.NewLocalToolRegistry()
	}
	return &StubToolRegistry{}
}

// ProvideToolExecutor returns a TavilyToolExecutor when a Tavily key is
// configured, else the no-op StubToolExecutor.
func ProvideToolExecutor(cfg *Config) port.ToolExecutorPort {
	if cfg.Tavily.APIKey != "" {
		return magi.NewTavilyToolExecutor(cfg.Tavily.APIKey)
	}
	return &StubToolExecutor{}
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
	broker *appserver.EventBroker,
	configs []*entity.MagiConfig,
	repo port.Repository,
) *orchestration.Orchestrator {
	return orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: agentLoop,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: commander,
		EventPub:  broker,
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		Configs:   configs,
		Policy:    consensus.DefaultConsensusPolicy(),
	})
}

func provideRunManager(orch *orchestration.Orchestrator) *decision.RunManager {
	return decision.NewRunManager(orch)
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

func provideReplayService(broker *appserver.EventBroker) *replay.Service {
	return replay.NewService(broker)
}

func provideMemoryService() *memory.Service {
	return memory.NewService(nil, nil)
}

func provideToolService(toolReg port.ToolRegistryPort) *tool.Service {
	return tool.NewService(toolReg)
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

func registerLifecycle(h *hzserver.Hertz) {
	// Routes are registered via fx.Invoke(appserver.RegisterRoutes).
	// This function ensures the server is created; lifecycle is in provideServer.
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
	db, err := gorm.Open(MysqlDialector(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}
	models := append(magi.AllModels(), rag.AllModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}
	return db, nil
}

func provideRepository(db *gorm.DB) port.Repository {
	return magi.NewRepository(db)
}
