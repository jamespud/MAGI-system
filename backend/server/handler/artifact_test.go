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
