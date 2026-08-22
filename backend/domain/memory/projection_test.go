package memory

import (
	"strings"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

func TestBuildProjection_SetsCaseID(t *testing.T) {
	proj := BuildProjection(
		&entity.DecisionCase{ID: "c1", Question: "q", Context: "ctx"},
		&entity.Resolution{FinalReport: "report", Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval}},
		nil,
		nil,
		nil,
	)
	if proj == nil || proj.CaseID != "c1" {
		t.Fatalf("CaseID missing: %+v", proj)
	}
	if proj.QuestionSummary != "q" || proj.Resolution != "report" {
		t.Fatalf("fields: %+v", proj)
	}
}

func TestBuildProjection_IndexDocKeepsFullBackground(t *testing.T) {
	// Background longer than the 200-rune display cap must survive in IndexDoc
	// and must NOT be clobbered by the resolution report.
	bg := strings.Repeat("背", 300)
	proj := BuildProjection(
		&entity.DecisionCase{ID: "c1", Question: "q", Context: bg},
		&entity.Resolution{FinalReport: "report", Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval}},
		nil, nil, nil,
	)
	if !strings.Contains(proj.IndexDoc, bg) {
		t.Error("IndexDoc lost the full background")
	}
	if len([]rune(proj.ContextSummary)) > 200 {
		t.Errorf("display ContextSummary must stay truncated, got %d runes", len([]rune(proj.ContextSummary)))
	}
}

func TestBuildProjection_IndexDocKeepsFullResolution(t *testing.T) {
	res := strings.Repeat("决", 1000)
	proj := BuildProjection(
		&entity.DecisionCase{ID: "c1", Question: "q", Context: "bg"},
		&entity.Resolution{FinalReport: res, Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval}},
		nil, nil, nil,
	)
	if !strings.Contains(proj.IndexDoc, res) {
		t.Error("IndexDoc lost the full resolution")
	}
	if len([]rune(proj.Resolution)) > 500 {
		t.Errorf("display Resolution must stay truncated, got %d runes", len([]rune(proj.Resolution)))
	}
}

func TestBuildProjection_IndexDocRetainsBackgroundWhenResolutionPresent(t *testing.T) {
	// Regression: the display ContextSummary is overwritten by the report,
	// but the original background must remain in the index document.
	proj := BuildProjection(
		&entity.DecisionCase{ID: "c1", Question: "q", Context: "original background"},
		&entity.Resolution{FinalReport: "report", Consensus: entity.ConsensusResult{Outcome: entity.ConsensusStrongApproval}},
		nil, nil, nil,
	)
	if !strings.Contains(proj.IndexDoc, "original background") {
		t.Error("IndexDoc must keep the original background even when resolution is present")
	}
}
