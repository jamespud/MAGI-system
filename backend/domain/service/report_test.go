package service_test

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/service"
)

func TestRenderReport_FullFields(t *testing.T) {
	d := &entity.FinalReportData{
		Decision:       "approve",
		Summary:        "the team should proceed",
		KeyReasons:     []string{"r1", "r2"},
		Risks:          []string{"risk1"},
		NextSteps:      []string{"step1"},
		KeyEvidenceIDs: []string{"EV-001", "EV-002"},
		KeyClaimIDs:    []string{"CL-001"},
	}
	out := service.RenderReport(d)
	for _, want := range []string{"# Decision Report", "approve", "the team should proceed", "- r1", "- r2", "- risk1", "- step1", "## Key Reasons", "## Risks", "## Next Steps", "## Key Evidence", "EV-001", "EV-002", "## Key Claims", "CL-001"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q, got:\n%s", want, out)
		}
	}
}

func TestRenderReport_EmptySectionsOmitted(t *testing.T) {
	d := &entity.FinalReportData{
		Decision: "reject",
		Summary:  "no",
	}
	out := service.RenderReport(d)
	if !strings.Contains(out, "reject") || !strings.Contains(out, "no") {
		t.Fatalf("missing decision/summary: %s", out)
	}
	for _, section := range []string{"## Key Reasons", "## Risks", "## Next Steps", "## Key Evidence", "## Key Claims"} {
		if strings.Contains(out, section) {
			t.Fatalf("empty section %q should be omitted: %s", section, out)
		}
	}
}

func TestRenderReport_NilSafe(t *testing.T) {
	if got := service.RenderReport(nil); got != "" {
		t.Fatalf("nil should render empty string, got %q", got)
	}
}
