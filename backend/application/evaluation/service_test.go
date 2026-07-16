package evaluation_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/application/evaluation"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/runtime"
)

func TestEvaluationService_Evaluate(t *testing.T) {
	svc := evaluation.NewService()
	results := []*runtime.LoopResult{
		{Status: runtime.LoopStatusCompleted},
		{Status: runtime.LoopStatusCompleted},
	}
	ev, err := svc.Evaluate(context.Background(), results, 1, entity.ConsensusStrongApproval)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil evaluation")
	}
	if !ev.FirstRoundConsensus {
		t.Fatal("expected first-round consensus")
	}
}
