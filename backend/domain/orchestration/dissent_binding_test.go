package orchestration_test

import (
	"context"
	"strings"
	"testing"

	magiapp "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// dissentCase builds an orchestrator backed by a real sqlite repository so the
// persisted resolution and the in-memory result can be compared.
func dissentCase(t *testing.T, votes map[string][]*entity.Vote) (orchestration.OrchestratorDeps, port.Repository, *gorm.DB, *mockMagiRuntime) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magiapp.AllModels()...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	repo := magiapp.NewRepository(db)
	mrt := newMockMagiRuntime()
	mrt.votes = votes
	deps := orchestration.OrchestratorDeps{
		AgentLoop: mrt,
		Consensus: consensus.NewConsensusEngine(),
		Debate:    debate.NewDebateEngine(nil),
		Commander: newCommander(t),
		CaseRepo:  repo.CaseRepo(),
		Repo:      repo,
		EventPub:  magiapp.NewEventPublisherAdapter(repo.EventRepo()),
		Configs:   []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:    consensus.DefaultConsensusPolicy(),
	}
	return deps, repo, db, mrt
}

func dissentReject() *entity.Vote {
	return &entity.Vote{
		Decision:         entity.VoteDecisionReject,
		Confidence:       70,
		ReasoningSummary: "unproven worst case",
		EvidenceIDs:      []string{"EV-001"},
		Conditions:       []entity.DecisionCondition{{Statement: "require rollback plan", MustHold: true}},
	}
}

// Issue #13 / CAS-8: the minority position must be bound to the resolution at
// solve time and persisted, so the report, HTTP and export all read one
// authoritative record instead of re-deriving (or dropping) dissent.
func TestOrchestrate_PersistsAuthoritativeDissent(t *testing.T) {
	deps, repo, _, _ := dissentCase(t, map[string][]*entity.Vote{
		"melchior":  {approve()},
		"balthasar": {approve()},
		"casper":    {dissentReject()},
	})
	orch := orchestration.NewOrchestrator(deps)
	ctx := context.Background()
	case_ := &entity.DecisionCase{ID: "c-dissent", Question: "compute", MaxDebateRounds: 1}
	if err := repo.CaseRepo().Create(ctx, case_); err != nil {
		t.Fatalf("create case: %v", err)
	}

	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusMajorityApprovalDissent {
		t.Fatalf("expected majority approval with dissent, got %+v", res)
	}
	if res.FinalDecision != entity.VoteDecisionApprove {
		t.Fatalf("final decision = %s", res.FinalDecision)
	}
	if len(res.Dissent) != 1 {
		t.Fatalf("want exactly one persisted dissenter, got %+v", res.Dissent)
	}
	d := res.Dissent[0]
	if d.AgentCode != entity.MagiCodeCasper || d.Decision != entity.VoteDecisionReject {
		t.Fatalf("dissent attribution: %+v", d)
	}
	if d.Reasoning != "unproven worst case" || len(d.Conditions) != 1 {
		t.Fatalf("dissent content lost: %+v", d)
	}
	if !strings.Contains(res.FinalReport, "--- Dissent ---") || !strings.Contains(res.FinalReport, "casper dissented with reject") {
		t.Fatalf("canonical report must embed the minority position:\n%s", res.FinalReport)
	}

	// The persisted resolution is the single source: reload and compare.
	got, err := repo.ResolutionRepo().Get(ctx, "c-dissent")
	if err != nil || got == nil {
		t.Fatalf("reload resolution: %+v err=%v", got, err)
	}
	if len(got.Dissent) != 1 || got.Dissent[0].AgentCode != entity.MagiCodeCasper || got.Dissent[0].Reasoning != "unproven worst case" {
		t.Fatalf("persisted dissent = %+v", got.Dissent)
	}
	if got.FinalReport != res.FinalReport {
		t.Fatalf("persisted report diverged from the returned report")
	}
	// Dissent evidence/claim references must resolve to persisted artifacts.
	evSet := map[string]bool{}
	evs, _ := repo.EvidenceRepo().ListByCase(ctx, "c-dissent")
	for _, e := range evs {
		evSet[e.ID] = true
	}
	for _, id := range got.Dissent[0].EvidenceIDs {
		if !evSet[id] {
			t.Fatalf("dissent cites unknown evidence %q", id)
		}
	}
}

func TestOrchestrate_UnanimousHasNoDissent(t *testing.T) {
	deps, repo, _, _ := dissentCase(t, map[string][]*entity.Vote{
		"melchior":  {approve()},
		"balthasar": {approve()},
		"casper":    {approve()},
	})
	orch := orchestration.NewOrchestrator(deps)
	ctx := context.Background()
	case_ := &entity.DecisionCase{ID: "c-unanimous", Question: "compute", MaxDebateRounds: 1}
	if err := repo.CaseRepo().Create(ctx, case_); err != nil {
		t.Fatalf("create case: %v", err)
	}

	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(res.Dissent) != 0 {
		t.Fatalf("a unanimous round must not fabricate dissent: %+v", res.Dissent)
	}
	if strings.Contains(res.FinalReport, "--- Dissent ---") {
		t.Fatalf("unanimous report must not carry a dissent section:\n%s", res.FinalReport)
	}
}

// The frozen citation set handed to the report model must be the current
// execution generation's namespaced artifacts, not an arbitrary string set:
// every cited id carries this case's prefix and the run phase it was produced
// under, and the report only cites ids from that same set.
func TestOrchestrate_ReportCitationsAreGenerationScoped(t *testing.T) {
	deps, repo, _, _ := dissentCase(t, map[string][]*entity.Vote{
		"melchior":  {approve()},
		"balthasar": {approve()},
		"casper":    {approve()},
	})
	orch := orchestration.NewOrchestrator(deps)
	ctx := context.Background()
	case_ := &entity.DecisionCase{ID: "c-cite", Question: "compute", MaxDebateRounds: 1}
	if err := repo.CaseRepo().Create(ctx, case_); err != nil {
		t.Fatalf("create case: %v", err)
	}
	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if len(res.KeyEvidenceIDs) == 0 || len(res.KeyClaimIDs) == 0 {
		t.Fatalf("expected durable citations, got evidence=%v claims=%v", res.KeyEvidenceIDs, res.KeyClaimIDs)
	}
	allowed := map[string]bool{}
	for _, id := range append(append([]string{}, res.KeyEvidenceIDs...), res.KeyClaimIDs...) {
		if !strings.HasPrefix(id, "c-cite-") {
			t.Fatalf("citation %q is not namespaced to this case/generation", id)
		}
		allowed[id] = true
	}
	for _, id := range findIDs(res.FinalReport) {
		if !allowed[id] {
			t.Fatalf("report cites %q which is not in the current generation's frozen set", id)
		}
	}
}
