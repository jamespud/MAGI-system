package handler_test

import (
	"context"
	"testing"
	"time"

	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/server/handler"
)

// memEvidenceRepo is a tiny in-memory EvidenceRepository for handler tests.
type memEvidenceRepo struct{ items []*entity.EvidenceRecord }

func (r *memEvidenceRepo) Create(ctx context.Context, e *entity.EvidenceRecord) error {
	r.items = append(r.items, e)
	return nil
}
func (r *memEvidenceRepo) Get(ctx context.Context, id string) (*entity.EvidenceRecord, error) {
	return nil, nil
}
func (r *memEvidenceRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.EvidenceRecord, error) {
	var out []*entity.EvidenceRecord
	for _, e := range r.items {
		if e.CaseID == caseID {
			out = append(out, e)
		}
	}
	return out, nil
}

var _ port.EvidenceRepository = (*memEvidenceRepo)(nil)

func TestArtifactHandler_Evidence(t *testing.T) {
	repo := &memEvidenceRepo{items: []*entity.EvidenceRecord{
		{ID: "EV-001", CaseID: "c1", Observation: "obs1", Reliability: entity.ReliabilityScore{Final: 0.9}, CollectedBy: entity.MagiCode("melchior"), CreatedAt: time.Now()},
		{ID: "EV-002", CaseID: "c1", Observation: "obs2", Reliability: entity.ReliabilityScore{Final: 0.8}, CollectedBy: entity.MagiCode("balthasar"), CreatedAt: time.Now()},
	}}
	svc := decision.NewService(nil, decision.ServiceConfig{}, decision.WithEvidenceRepo(repo))
	h := handler.NewArtifactHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/cases/:id/evidence", h.Evidence)

	w := ut.PerformRequest(r.Engine, "GET", "/cases/c1/evidence", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d", resp.StatusCode())
	}
	body := string(resp.Body())
	if !contains(body, "EV-001") || !contains(body, "melchior") {
		t.Fatalf("response missing evidence: %s", body)
	}
}

func TestArtifactHandler_Evidence_EmptyForUnknownCase(t *testing.T) {
	repo := &memEvidenceRepo{}
	svc := decision.NewService(nil, decision.ServiceConfig{}, decision.WithEvidenceRepo(repo))
	h := handler.NewArtifactHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/cases/:id/evidence", h.Evidence)

	w := ut.PerformRequest(r.Engine, "GET", "/cases/unknown/evidence", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d", resp.StatusCode())
	}
	if string(resp.Body()) != "[]" {
		t.Fatalf("expected empty array, got %s", string(resp.Body()))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// --- additional stub repos for /agents test ---

type memClaimRepo struct{ items []*entity.Claim }

func (r *memClaimRepo) Create(ctx context.Context, c *entity.Claim) error { r.items = append(r.items, c); return nil }
func (r *memClaimRepo) Get(ctx context.Context, id string) (*entity.Claim, error) { return nil, nil }
func (r *memClaimRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.Claim, error) {
	var out []*entity.Claim
	for _, c := range r.items {
		if c.CaseID == caseID {
			out = append(out, c)
		}
	}
	return out, nil
}

type memAgentRunRepo struct{ items []*entity.AgentRun }

func (r *memAgentRunRepo) Create(ctx context.Context, a *entity.AgentRun) error { r.items = append(r.items, a); return nil }
func (r *memAgentRunRepo) Get(ctx context.Context, id string) (*entity.AgentRun, error) { return nil, nil }
func (r *memAgentRunRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.AgentRun, error) {
	var out []*entity.AgentRun
	for _, a := range r.items {
		if a.CaseID == caseID {
			out = append(out, a)
		}
	}
	return out, nil
}

type memToolCallRepo struct{ items []*entity.ToolCall }

func (r *memToolCallRepo) Create(ctx context.Context, t *entity.ToolCall) error { r.items = append(r.items, t); return nil }
func (r *memToolCallRepo) ListByCase(ctx context.Context, caseID string) ([]*entity.ToolCall, error) {
	var out []*entity.ToolCall
	for _, t := range r.items {
		if t.CaseID == caseID {
			out = append(out, t)
		}
	}
	return out, nil
}

func TestArtifactHandler_AgentsReturnsArrays(t *testing.T) {
	evRepo := &memEvidenceRepo{items: []*entity.EvidenceRecord{
		{ID: "EV-m1", CaseID: "c1", Observation: "obs", Reliability: entity.ReliabilityScore{Final: 0.9}, CollectedBy: entity.MagiCode("melchior"), CreatedAt: time.Now()},
	}}
	clRepo := &memClaimRepo{items: []*entity.Claim{
		{ID: "CL-m1", CaseID: "c1", Statement: "claim text", CreatedBy: entity.MagiCode("melchior"), CreatedAt: time.Now()},
	}}
	tcRepo := &memToolCallRepo{items: []*entity.ToolCall{
		{ID: "tc1", CaseID: "c1", AgentRunID: "run-m1", ToolCallID: "call-1", ToolName: "calc", Arguments: "{}", Valid: true, Result: "3", DurationMs: 5, CreatedAt: time.Now()},
	}}
	runRepo := &memAgentRunRepo{items: []*entity.AgentRun{
		{ID: "run-m1", CaseID: "c1", MagiCode: entity.MagiCode("melchior"), Round: 1, Status: entity.AgentRunStatusCompleted, StartedAt: time.Now()},
	}}
	svc := decision.NewService(nil, decision.ServiceConfig{},
		decision.WithEvidenceRepo(evRepo),
		decision.WithClaimRepo(clRepo),
		decision.WithAgentRunRepo(runRepo),
		decision.WithToolCallRepo(tcRepo))
	h := handler.NewArtifactHandler(svc)

	r := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	r.GET("/cases/:id/agents", h.Agents)

	w := ut.PerformRequest(r.Engine, "GET", "/cases/c1/agents", nil)
	resp := w.Result()
	if resp.StatusCode() != 200 {
		t.Fatalf("status: %d body=%s", resp.StatusCode(), string(resp.Body()))
	}
	body := string(resp.Body())
	if !contains(body, `"tool_calls"`) || !contains(body, "calc") {
		t.Fatalf("response missing tool_calls/calc: %s", body)
	}
	if !contains(body, `"evidence"`) || !contains(body, "EV-m1") {
		t.Fatalf("response missing evidence array: %s", body)
	}
	if !contains(body, `"claims"`) || !contains(body, "claim text") {
		t.Fatalf("response missing claims array: %s", body)
	}
}
