package orchestration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	magiapp "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type stubKnowledge struct {
	stored []*entity.CaseMemoryProjection
}

func (k *stubKnowledge) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{}, nil
}

func (k *stubKnowledge) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	k.stored = append(k.stored, proj)
	return nil
}

func TestIntegration_OrchestratorWithDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magiapp.AllModels()...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	repo := magiapp.NewRepository(db)
	caseRepo := repo.CaseRepo()
	eventRepo := repo.EventRepo()

	gen := validation.NewReflectSchemaGenerator()
	val := validation.NewJSONSchemaValidator()
	cmdModel := &scriptedChatModel{responses: []*schema.Message{
		schema.AssistantMessage(`{"canonical_question":"compute"}`, nil),
		schema.AssistantMessage(`{"decision":"approve","summary":"decision report from db test","key_reasons":["r"],"risks":[],"next_steps":[],"key_evidence_ids":["EV-001"]}`, nil),
	}}
	cmd, err := service.NewCommander(service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"}, &stubModelPort{m: cmdModel}, gen, val)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}

	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	knowledge := &stubKnowledge{}
	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: cmd,
		CaseRepo:  caseRepo,
		Repo:      repo,
		EventPub:  magiapp.NewEventPublisherAdapter(eventRepo),
		Knowledge: knowledge,
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	})

	case_ := &entity.DecisionCase{ID: "c1", Question: "compute", MaxDebateRounds: 1}
	ctx := context.Background()
	if err := caseRepo.Create(ctx, case_); err != nil {
		t.Fatalf("create case: %v", err)
	}

	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusStrongApproval {
		t.Fatalf("resolution: %+v", res)
	}

	dbCase, err := caseRepo.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	if dbCase.Status != entity.CaseStatusResolved {
		t.Fatalf("case status in DB: %s, want RESOLVED", dbCase.Status)
	}

	events, err := eventRepo.ListByCase(ctx, "c1")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected events in DB")
	}

	// Normalized task persisted into case.task_json.
	var cm magiapp.CaseModel
	if err := db.First(&cm, "id = ?", "c1").Error; err != nil {
		t.Fatalf("load case model: %v", err)
	}
	if cm.TaskJSON == "" {
		t.Fatal("expected task_json to be persisted")
	}

	// Agent runs persisted for the three agents.
	runs, err := repo.AgentRunRepo().ListByCase(ctx, "c1")
	if err != nil || len(runs) != 3 {
		t.Fatalf("agent runs: %v len=%d", err, len(runs))
	}

	// Resolution persisted with evidence-citing report and evaluation.
	resGot, err := repo.ResolutionRepo().Get(ctx, "c1")
	if err != nil || resGot == nil {
		t.Fatalf("resolution persisted: %v", err)
	}
	if !strings.Contains(resGot.FinalReport, "EV-001") {
		t.Fatalf("report missing evidence citation: %s", resGot.FinalReport)
	}
	if resGot.Evaluation == nil || resGot.Evaluation.ConsensusRound == 0 {
		t.Fatalf("evaluation not persisted: %+v", resGot.Evaluation)
	}

	// Memory projection written through the knowledge adapter.
	if len(knowledge.stored) != 1 || knowledge.stored[0].Resolution == "" {
		t.Fatalf("memory projection not stored via knowledge: %+v", knowledge.stored)
	}
}
