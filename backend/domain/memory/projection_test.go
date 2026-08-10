package memory

import (
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
