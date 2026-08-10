package orchestration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	magiapp "github.com/jamespud/magi/backend/adapter"
	rag "github.com/jamespud/magi/backend/adapter/rag"
	"github.com/jamespud/magi/backend/domain/consensus"
	"github.com/jamespud/magi/backend/domain/debate"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/orchestration"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// lifecycleFakeEmbedder is a deterministic Embedder for the audit test.
type lifecycleFakeEmbedder struct{ dim int }

func (f lifecycleFakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		v := make([]float32, f.dim)
		for j := range v {
			v[j] = float32(i) / 10.0
		}
		out[i] = v
	}
	return out, nil
}

// lifecycleVecIndex records upserts and returns all stored 300 chunks on
// search, so retrieval exercises the full merge path.
type lifecycleVecIndex struct {
	stored []rag.VectorRecord
}

func (r *lifecycleVecIndex) Upsert(_ context.Context, recs []rag.VectorRecord) error {
	r.stored = append(r.stored, recs...)
	return nil
}
func (r *lifecycleVecIndex) Search(_ context.Context, _ []float32, topK int, _ *rag.IndexFilter) ([]rag.VectorHit, error) {
	hits := make([]rag.VectorHit, 0, len(r.stored))
	for i, s := range r.stored {
		if i >= topK {
			break
		}
		hits = append(hits, rag.VectorHit{ChunkID: s.ChunkID, Score: 1})
	}
	return hits, nil
}
func (r *lifecycleVecIndex) DeleteBySourceRef(context.Context, string, string) error { return nil }

type lifecycleLexIndex struct {
	stored []rag.TextRecord
}

func (r *lifecycleLexIndex) Upsert(_ context.Context, recs []rag.TextRecord) error {
	r.stored = append(r.stored, recs...)
	return nil
}
func (r *lifecycleLexIndex) Search(_ context.Context, _ string, topK int, _ *rag.IndexFilter) ([]rag.TextHit, error) {
	hits := make([]rag.TextHit, 0, len(r.stored))
	for i, s := range r.stored {
		if i >= topK {
			break
		}
		hits = append(hits, rag.TextHit{ChunkID: s.ChunkID, Score: 1})
	}
	return hits, nil
}
func (r *lifecycleLexIndex) DeleteBySourceRef(context.Context, string, string) error { return nil }

// TestIntegration_OrchestratorPersistsCompleteLifecycle runs a full case
// through the real orchestrator + real RAG adapter and audits every artifact
// a resolved case must leave behind. It exists to catch silent wiring gaps
// (e.g. the memory projection row that was never persisted, leaving the
// Memory page search empty while RAG chunks were written).
func TestIntegration_OrchestratorPersistsCompleteLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	models := append(magiapp.AllModels(), rag.AllModels()...)
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	repo := magiapp.NewRepository(db)
	eventRepo := repo.EventRepo()
	eventPub := magiapp.NewEventPublisherAdapter(eventRepo)

	// Real RAG stack over sqlite with recording vector/lexical indexes.
	emb := lifecycleFakeEmbedder{dim: 3}
	vec := &lifecycleVecIndex{}
	lex := &lifecycleLexIndex{}
	ch := rag.NewChunker(rag.RuneTokenCounter{CharsPerToken: 1}, rag.ChunkLevels{L1800: 300, L900: 150, L300: 50})
	chunkRepo := rag.NewChunkRepository(db)
	retriever := rag.NewRetriever(vec, lex, emb, chunkRepo, rag.MergeOpts{TopK: 15, RRFK: 60, Thr900: 3, Thr1800: 2, Orphan: "keep_300"})
	knowledge := rag.NewHybridKnowledgeAdapter(ch, emb, chunkRepo, vec, lex, retriever, eventPub)

	mrt := newMockMagiRuntime()
	mrt.votes["melchior"] = []*entity.Vote{approve()}
	mrt.votes["balthasar"] = []*entity.Vote{approve()}
	mrt.votes["casper"] = []*entity.Vote{approve()}

	orch := orchestration.NewOrchestrator(orchestration.OrchestratorDeps{
		AgentLoop:  mrt,
		Consensus:  consensus.NewConsensusEngine(),
		Debate:     debate.NewDebateEngine(nil),
		Commander:  newCommander(t),
		CaseRepo:   repo.CaseRepo(),
		Repo:       repo,
		MemoryRepo: repo.MemoryRepo(),
		EventPub:   eventPub,
		Knowledge:  knowledge,
		Configs:    []*entity.MagiConfig{magiCfg("melchior"), magiCfg("balthasar"), magiCfg("casper")},
		Policy:     consensus.DefaultConsensusPolicy(),
	})

	case_ := &entity.DecisionCase{ID: "c-audit", Question: "compute full lifecycle audit", MaxDebateRounds: 1}
	ctx := context.Background()
	if err := repo.CaseRepo().Create(ctx, case_); err != nil {
		t.Fatalf("create case: %v", err)
	}
	res, err := orch.Orchestrate(ctx, case_)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if res == nil || res.Consensus.Outcome != entity.ConsensusStrongApproval {
		t.Fatalf("resolution: %+v", res)
	}

	// 1. Case resolved.
	dbCase, err := repo.CaseRepo().Get(ctx, "c-audit")
	if err != nil || dbCase.Status != entity.CaseStatusResolved {
		t.Fatalf("case status: %+v err=%v", dbCase, err)
	}

	// 2. Resolution persisted with report + evaluation.
	resGot, err := repo.ResolutionRepo().Get(ctx, "c-audit")
	if err != nil || resGot == nil || resGot.FinalReport == "" || resGot.Evaluation == nil {
		t.Fatalf("resolution persisted: %+v err=%v", resGot, err)
	}

	// 3. One agent run per agent.
	runs, err := repo.AgentRunRepo().ListByCase(ctx, "c-audit")
	if err != nil || len(runs) != 3 {
		t.Fatalf("agent runs: %d err=%v", len(runs), err)
	}

	// 4. Votes persisted.
	votes, err := repo.VoteRepo().ListByCase(ctx, "c-audit")
	if err != nil || len(votes) != 3 {
		t.Fatalf("votes: %d err=%v", len(votes), err)
	}

	// 5. Claims persisted (one per agent).
	claims, err := repo.ClaimRepo().ListByCase(ctx, "c-audit")
	if err != nil || len(claims) != 3 {
		t.Fatalf("claims: %d err=%v", len(claims), err)
	}

	// 6. Evidence persisted (one per agent).
	evs, err := repo.EvidenceRepo().ListByCase(ctx, "c-audit")
	if err != nil || len(evs) != 3 {
		t.Fatalf("evidence: %d err=%v", len(evs), err)
	}
	evIDSet := make(map[string]bool, len(evs))
	clIDSet := make(map[string]bool, len(claims))
	for _, e := range evs {
		evIDSet[e.ID] = true
	}
	for _, c := range claims {
		clIDSet[c.ID] = true
		for _, sid := range c.Supports {
			if !evIDSet[sid] {
				t.Fatalf("claim %s supports unknown evidence %q", c.ID, sid)
			}
		}
	}
	// Every ID the resolution cites must resolve to a persisted artifact row.
	for _, id := range resGot.KeyEvidenceIDs {
		if !evIDSet[id] {
			t.Fatalf("resolution cites unknown evidence %q", id)
		}
	}
	for _, id := range resGot.KeyClaimIDs {
		if !clIDSet[id] {
			t.Fatalf("resolution cites unknown claim %q", id)
		}
	}
	// The report body must cite persisted namespaced IDs (each citation
	// resolves to a row in the evidence/claim tables, checked below).
	reportIDs := findIDs(resGot.FinalReport)
	if len(reportIDs) == 0 {
		t.Fatalf("report does not cite any EV-ID: %s", resGot.FinalReport)
	}
	for _, id := range reportIDs {
		if !evIDSet[id] && !clIDSet[id] {
			t.Fatalf("report cites unknown ID %q", id)
		}
	}
	// Votes must reference persisted evidence/claims.
	for _, v := range votes {
		for _, id := range v.EvidenceIDs {
			if !evIDSet[id] {
				t.Fatalf("vote references unknown evidence %q", id)
			}
		}
		for _, id := range v.KeyClaimIDs {
			if !clIDSet[id] {
				t.Fatalf("vote references unknown claim %q", id)
			}
		}
	}

	// 7. Tool calls persisted (one per agent).
	toolCalls, err := repo.ToolCallRepo().ListByCase(ctx, "c-audit")
	if err != nil || len(toolCalls) != 3 {
		t.Fatalf("tool calls: %d err=%v", len(toolCalls), err)
	}

	// 8. Memory projection persisted and searchable (regression: was never saved).
	proj, err := repo.MemoryRepo().Get(ctx, "c-audit")
	if err != nil || proj == nil {
		t.Fatalf("memory projection persisted: %+v err=%v", proj, err)
	}
	if !strings.Contains(proj.QuestionSummary, "compute full lifecycle audit") || proj.Resolution == "" || len(proj.Votes) != 3 {
		t.Fatalf("memory projection content: %+v", proj)
	}
	// Memory projection must carry namespaced IDs and role-attributed votes.
	for _, ev := range proj.KeyEvidence {
		if !evIDSet[ev.EvidenceID] {
			t.Fatalf("memory projection references unknown evidence %q", ev.EvidenceID)
		}
	}
	for _, v := range proj.Votes {
		if v.MagiCode != entity.MagiCodeMelchior && v.MagiCode != entity.MagiCodeBalthasar && v.MagiCode != entity.MagiCodeCasper {
			t.Fatalf("memory projection vote missing role: %+v", v)
		}
	}
	searchRes, err := repo.MemoryRepo().Search(ctx, "lifecycle audit", 10)
	if err != nil || len(searchRes) == 0 || searchRes[0].CaseID != "c-audit" {
		t.Fatalf("memory search: %+v err=%v", searchRes, err)
	}

	// 9. RAG chunks persisted at all three levels.
	for _, m := range []any{&rag.Chunk300{}, &rag.Chunk900{}, &rag.Chunk1800{}} {
		var n int64
		db.Model(m).Count(&n)
		if n == 0 {
			t.Fatalf("expected %T rows after store", m)
		}
	}

	// 10. MEMORY_INDEXED event carries chunk stats matching the DB, and the
	// case completed event is present.
	events, err := eventRepo.ListByCase(ctx, "c-audit")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	var memEvent *entity.MagiEvent
	completed := false
	for i := range events {
		switch events[i].Type {
		case entity.EventMemoryIndexed:
			memEvent = events[i]
		case entity.EventCaseCompleted:
			completed = true
		}
	}
	if memEvent == nil {
		t.Fatal("expected MEMORY_INDEXED event in DB")
	}
	var payload map[string]any
	if err := json.Unmarshal(memEvent.Payload, &payload); err != nil {
		t.Fatalf("memory event payload: %v", err)
	}
	var n300 int64
	db.Model(&rag.Chunk300{}).Count(&n300)
	if payload["chunks_300"] != float64(n300) || payload["chunks_300"] == float64(0) {
		t.Fatalf("memory event payload mismatch: %v vs %d chunks", payload, n300)
	}
	if !completed {
		t.Fatal("expected CASE_COMPLETED event in DB")
	}

	// 11. RAG retrieval returns blocks for a query close to the indexed memory.
	retr, err := knowledge.Retrieve(ctx, port.RetrieveRequest{Query: "compute", TopK: 15})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(retr.Blocks) == 0 {
		t.Fatal("expected retrieval blocks from stored memory")
	}
}

// findIDs extracts EV-/CL- artifact IDs from free text (report citations).
// Persisted IDs are namespaced ("<case>-<code>-r<n>-<phase>-EV-001"), so the
// matcher recognizes both the bare in-memory form and the full persisted form.
func findIDs(s string) []string {
	var out []string
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '"' || r == '[' || r == ']' || r == '\n' || r == ':' || r == '(' || r == ')'
	})
	for _, f := range fields {
		f = strings.Trim(f, `",.`)
		if strings.Contains(f, "EV-") || strings.Contains(f, "CL-") {
			out = append(out, f)
		}
	}
	return out
}
