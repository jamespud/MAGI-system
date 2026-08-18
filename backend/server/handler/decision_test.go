package handler_test

import (
	"context"
	"testing"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/handler"
)

// fakeRunManager stubs the methods DecisionHandler calls.
type fakeRunManager struct {
	started   map[string]bool
	cancelled map[string]bool
}

func newFakeRunManager() *fakeRunManager {
	return &fakeRunManager{started: map[string]bool{}, cancelled: map[string]bool{}}
}

func (f *fakeRunManager) Start(ctx context.Context, c *entity.DecisionCase) error {
	if f.started[c.ID] {
		return decision.ErrAlreadyRunning
	}
	f.started[c.ID] = true
	return nil
}
func (f *fakeRunManager) Cancel(caseID string) bool {
	if f.started[caseID] {
		f.cancelled[caseID] = true
		return true
	}
	return false
}
func (f *fakeRunManager) Pause(caseID string) bool     { return f.started[caseID] }
func (f *fakeRunManager) Resume(caseID string) bool    { return false }
func (f *fakeRunManager) IsRunning(caseID string) bool { return f.started[caseID] }

// stubCaseLookup implements port.CaseRepository enough for the handler.
type stubCaseLookup struct{ c *entity.DecisionCase }

func (s stubCaseLookup) Create(ctx context.Context, c *entity.DecisionCase) error { return nil }
func (s stubCaseLookup) Get(ctx context.Context, id string) (*entity.DecisionCase, error) {
	if s.c != nil && s.c.ID == id {
		return s.c, nil
	}
	return nil, nil
}
func (s stubCaseLookup) ListPaged(ctx context.Context, userID int64, page, pageSize int) ([]*entity.DecisionCase, int64, error) {
	return nil, 0, nil
}
func (s stubCaseLookup) UpdateFlags(ctx context.Context, id string, pinned, archived *bool) error {
	return nil
}
func (s stubCaseLookup) Delete(ctx context.Context, id string) error { return nil }

func (s stubCaseLookup) List(ctx context.Context) ([]*entity.DecisionCase, error) { return nil, nil }
func (s stubCaseLookup) UpdateStatus(ctx context.Context, id string, st entity.CaseStatus) error {
	if s.c != nil {
		s.c.Status = st
	}
	return nil
}
func (s stubCaseLookup) UpdateTask(ctx context.Context, id string, task *entity.DecisionTask) error {
	return nil
}

// Compile-time: stubCaseLookup satisfies port.CaseRepository.
var _ port.CaseRepository = stubCaseLookup{}

// Compile-time: fakeRunManager satisfies decision.RunController.
var _ decision.RunController = (*fakeRunManager)(nil)

func TestDecisionHandler_Run_Returns202(t *testing.T) {
	rm := newFakeRunManager()
	c := &entity.DecisionCase{ID: "c1", Question: "q", Status: entity.CaseStatusDraft}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithRunManager(rm),
		decision.WithCaseRepo(stubCaseLookup{c: c}))
	h := handler.NewDecisionHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.POST("/cases/:id/run", h.Run)

	w := ut.PerformRequest(r.Engine, "POST", "/cases/c1/run", nil)
	resp := w.Result()
	if resp.StatusCode() != 202 {
		t.Fatalf("expected 202, got %d body=%s", resp.StatusCode(), string(resp.Body()))
	}
}

func TestDecisionHandler_Run_Returns409WhenDuplicate(t *testing.T) {
	rm := newFakeRunManager()
	rm.started["c1"] = true // already running
	c := &entity.DecisionCase{ID: "c1", Question: "q"}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithRunManager(rm),
		decision.WithCaseRepo(stubCaseLookup{c: c}))
	h := handler.NewDecisionHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.POST("/cases/:id/run", h.Run)

	w := ut.PerformRequest(r.Engine, "POST", "/cases/c1/run", nil)
	if w.Result().StatusCode() != 409 {
		t.Fatalf("expected 409, got %d", w.Result().StatusCode())
	}
}

func TestDecisionHandler_Run_Returns404WhenCaseMissing(t *testing.T) {
	rm := newFakeRunManager()
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithRunManager(rm),
		decision.WithCaseRepo(stubCaseLookup{})) // no case
	h := handler.NewDecisionHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.POST("/cases/:id/run", h.Run)

	w := ut.PerformRequest(r.Engine, "POST", "/cases/missing/run", nil)
	if w.Result().StatusCode() != 404 {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode())
	}
}

func TestDecisionHandler_PauseAndResume(t *testing.T) {
	rm := newFakeRunManager()
	c := &entity.DecisionCase{ID: "c1", Question: "q", Status: entity.CaseStatusInvestigating}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithRunManager(rm),
		decision.WithCaseRepo(stubCaseLookup{c: c}))
	h := handler.NewDecisionHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.POST("/cases/:id/pause", h.Pause)
	r.POST("/cases/:id/resume", h.Resume)

	w := ut.PerformRequest(r.Engine, "POST", "/cases/c1/pause", nil)
	if w.Result().StatusCode() != 200 {
		t.Fatalf("pause: expected 200, got %d body=%s", w.Result().StatusCode(), string(w.Result().Body()))
	}
	w = ut.PerformRequest(r.Engine, "POST", "/cases/c1/resume", nil)
	if w.Result().StatusCode() != 200 {
		t.Fatalf("resume: expected 200, got %d body=%s", w.Result().StatusCode(), string(w.Result().Body()))
	}
}

func TestDecisionHandler_PauseRejectsResolvedCase(t *testing.T) {
	rm := newFakeRunManager()
	c := &entity.DecisionCase{ID: "c1", Question: "q", Status: entity.CaseStatusResolved}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithRunManager(rm),
		decision.WithCaseRepo(stubCaseLookup{c: c}))
	h := handler.NewDecisionHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.POST("/cases/:id/pause", h.Pause)

	w := ut.PerformRequest(r.Engine, "POST", "/cases/c1/pause", nil)
	if w.Result().StatusCode() != 400 {
		t.Fatalf("expected 400 for resolved case, got %d", w.Result().StatusCode())
	}
}
