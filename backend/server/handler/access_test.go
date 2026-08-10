package handler_test

import (
	"context"
	"strings"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/admin"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/handler"
)

type ownedCaseRepo struct {
	case_ *entity.DecisionCase
}

func (r *ownedCaseRepo) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (r *ownedCaseRepo) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	if r.case_ != nil && r.case_.ID == id {
		return r.case_, nil
	}
	return nil, nil
}
func (r *ownedCaseRepo) List(ctx context.Context) ([]*entity.DecisionCase, error) {
	if r.case_ != nil {
		return []*entity.DecisionCase{r.case_}, nil
	}
	return nil, nil
}
func (r *ownedCaseRepo) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	return nil
}
func (r *ownedCaseRepo) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

func TestDecisionHandler_EnforcesCaseOwnership(t *testing.T) {
	svc := decision.NewService(nil, decision.ServiceConfig{}, decision.WithCaseRepo(&ownedCaseRepo{
		case_: &entity.DecisionCase{ID: "c1", UserID: 7},
	}))
	h := handler.NewDecisionHandler(svc)
	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "user3", Key: "k3", UserID: 3, Role: "user"},
		{Name: "owner7", Key: "k7", UserID: 7, Role: "admin"},
	})
	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.Use(server.Auth(authSvc))
	r.GET("/cases/:id", h.Get)

	w := ut.PerformRequest(r.Engine, "GET", "/cases/c1", nil, ut.Header{Key: "Authorization", Value: "Bearer k3"})
	if w.Code != 403 {
		t.Fatalf("other user: expected 403, got %d body=%s", w.Code, w.Body.String())
	}
	w = ut.PerformRequest(r.Engine, "GET", "/cases/c1", nil, ut.Header{Key: "Authorization", Value: "Bearer k7"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"c1"`) {
		t.Fatalf("owner: expected 200 with case, got %d body=%s", w.Code, w.Body.String())
	}
}

type stubAdminAgentRuns struct{}

func (stubAdminAgentRuns) Create(ctx context.Context, r *entity.AgentRun) error { return nil }
func (stubAdminAgentRuns) Get(ctx context.Context, id string) (*entity.AgentRun, error) {
	return nil, nil
}
func (stubAdminAgentRuns) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	return nil, nil
}
func (stubAdminAgentRuns) SumUsageByUser(ctx context.Context, userID int64) (int64, float64, error) {
	return 0, 0, nil
}

func TestRequireRole_AdminGate(t *testing.T) {
	adminSvc := admin.NewService(&ownedCaseRepo{}, stubAdminAgentRuns{})
	authSvc := auth.NewService(true, []auth.KeySpec{
		{Name: "user", Key: "k-u", UserID: 1, Role: "user"},
		{Name: "boss", Key: "k-a", UserID: 2, Role: "admin"},
	})
	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.Use(server.Auth(authSvc))
	adminH := handler.NewAdminHandler(adminSvc)
	r.GET("/admin/usage", server.RequireRole("admin"), adminH.Usage)

	w := ut.PerformRequest(r.Engine, "GET", "/admin/usage", nil, ut.Header{Key: "Authorization", Value: "Bearer k-u"})
	if w.Code != 403 {
		t.Fatalf("user token: expected 403, got %d", w.Code)
	}
	w = ut.PerformRequest(r.Engine, "GET", "/admin/usage", nil, ut.Header{Key: "Authorization", Value: "Bearer k-a"})
	if w.Code != 200 {
		t.Fatalf("admin token: expected 200, got %d", w.Code)
	}
}
