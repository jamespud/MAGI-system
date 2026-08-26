package a2atransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// captureListTasksHandler embeds the full interface and only implements
// ListTasks, so it satisfies a2asrv.RequestHandler without enumerating every
// protocol method. The REST router dispatches only /tasks for ListTasks.
type captureListTasksHandler struct {
	a2asrv.RequestHandler
	got *time.Time
}

func (c *captureListTasksHandler) ListTasks(_ context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	c.got = req.StatusTimestampAfter
	return &a2a.ListTasksResponse{}, nil
}

func TestNormalizeListTasksQuery_AliasCopied(t *testing.T) {
	fake := &captureListTasksHandler{}
	h := normalizeListTasksQuery(a2asrv.NewRESTHandler(fake))
	after := time.Date(2026, 8, 26, 1, 2, 3, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet, "/tasks?lastUpdatedAfter="+url.QueryEscape(after.Format(time.RFC3339)), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if fake.got == nil || !fake.got.Equal(after) {
		t.Fatalf("StatusTimestampAfter = %v, want %v", fake.got, after)
	}
}

func TestNormalizeListTasksQuery_CanonicalWins(t *testing.T) {
	fake := &captureListTasksHandler{}
	h := normalizeListTasksQuery(a2asrv.NewRESTHandler(fake))
	canonical := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	alias := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	req := httptest.NewRequest(http.MethodGet,
		"/tasks?statusTimestampAfter="+url.QueryEscape(canonical.Format(time.RFC3339))+
			"&lastUpdatedAfter="+url.QueryEscape(alias.Format(time.RFC3339)), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if fake.got == nil || !fake.got.Equal(canonical) {
		t.Fatalf("StatusTimestampAfter = %v, want canonical %v", fake.got, canonical)
	}
}

func TestNormalizeListTasksQuery_NoAliasUnchanged(t *testing.T) {
	fake := &captureListTasksHandler{}
	h := normalizeListTasksQuery(a2asrv.NewRESTHandler(fake))
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if fake.got != nil {
		t.Fatalf("StatusTimestampAfter = %v, want nil when absent", fake.got)
	}
}
