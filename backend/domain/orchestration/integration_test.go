package orchestration_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
	magiapp "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/service"
	"github.com/jamespud/magi/backend/domain/validation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
		schema.AssistantMessage("decision report from db test", nil),
	}}
	cmd, err := service.NewCommander(service.CommanderConfig{Model: entity.ModelRef{ModelID: 1}, Persona: "commander"}, &stubModelPort{m: cmdModel}, gen, val)
	if err != nil {
		t.Fatalf("commander: %v", err)
	}

	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: cmd,
		CaseRepo:  caseRepo,
		EventPub:  magiapp.NewEventPublisherAdapter(eventRepo),
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
}
