package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/application/ragindex"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/handler"
	"time"
)

func TestAdminRagReindex_EnqueuesJobs(t *testing.T) {
	db := openE2EDB(t)
	repo := magi.NewRepository(db)

	// Seed one memory projection and one knowledge doc so reindex has rows.
	memRepo := repo.MemoryRepo()
	if err := memRepo.Save(context.Background(), &entity.CaseMemoryProjection{CaseID: "case-1", QuestionSummary: "q"}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}
	knowRepo := magi.NewKnowledgeRepository(db)
	if err := knowRepo.Create(context.Background(), &entity.KnowledgeDoc{ID: "kd-1", Title: "t", Content: "c"}); err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	jobRepo := magi.NewRagIndexJobRepository(db)
	svc := ragindex.NewService(jobRepo, memRepo, knowRepo, 3)

	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "admin", Key: "k7", UserID: 7, Role: "admin"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	server.RegisterRoutesWithDeps(h, server.RouteDeps{
		Auth:     authSvc,
		Metrics:  metrics.New(),
		AdminRag: handler.NewAdminRagHandler(svc),
	})

	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/rag/reindex?source=all", nil,
		ut.Header{Key: "Authorization", Value: "Bearer k7"})
	resp := w.Result()
	if resp.StatusCode() != http.StatusOK {
		body := string(resp.Body())
		t.Fatalf("status = %d, body = %s", resp.StatusCode(), body)
	}
	var out struct {
		Enqueued int    `json:"enqueued"`
		Source   string `json:"source"`
	}
	if err := json.Unmarshal(resp.Body(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Enqueued != 2 {
		t.Fatalf("enqueued = %d, want 2 (1 case_memory + 1 knowledge_doc)", out.Enqueued)
	}

	// Both jobs must be present and queued.
	runnable, err := jobRepo.ListRunnable(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("list runnable: %v", err)
	}
	if len(runnable) != 2 {
		t.Fatalf("runnable jobs = %d, want 2", len(runnable))
	}
}

func TestAdminRagReindex_RequiresAdmin(t *testing.T) {
	db := openE2EDB(t)
	repo := magi.NewRepository(db)
	jobRepo := magi.NewRagIndexJobRepository(db)
	svc := ragindex.NewService(jobRepo, repo.MemoryRepo(), magi.NewKnowledgeRepository(db), 3)

	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "admin", Key: "k7", UserID: 7, Role: "admin"},
		{Name: "u8", Key: "k8", UserID: 8, Role: "user"},
	})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	server.RegisterRoutesWithDeps(h, server.RouteDeps{
		Auth:     authSvc,
		Metrics:  metrics.New(),
		AdminRag: handler.NewAdminRagHandler(svc),
	})

	// Non-admin user must be forbidden.
	w := ut.PerformRequest(h.Engine, http.MethodPost, "/api/v1/admin/rag/reindex?source=case_memory", nil,
		ut.Header{Key: "Authorization", Value: "Bearer k8"})
	if got := w.Result().StatusCode(); got != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", got)
	}
}
