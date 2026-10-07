package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// T3 / #21: persisted artifact IDs use the durable Case execution generation,
// never the resettable ExecutionAttempt retry ordinal.
func TestExecutionRunIDUsesGenerationNamespace(t *testing.T) {
	got := executionRunID("case-1", "melchior", 7, 2, "investigate")
	want := "case-1-melchior-g7-r2-investigate"
	if got != want {
		t.Fatalf("executionRunID = %q, want %q", got, want)
	}
}

func TestOrchestrateForExecutionRequiresOwnedArtifactCapability(t *testing.T) {
	o := &Orchestrator{}
	c := &entity.DecisionCase{ID: "case-1", ExecutionGeneration: 3}
	owner := &entity.ExecutionContext{
		CaseID: "case-1", JobID: "job-1", WorkerID: "worker-1", ExecutionGeneration: 3,
	}
	if _, err := o.OrchestrateForExecution(context.Background(), c, owner); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("OrchestrateForExecution error = %v, want ErrLeaseLost", err)
	}
}

func TestOrchestrateRejectsPositiveGenerationWithoutOwner(t *testing.T) {
	o := &Orchestrator{}
	_, err := o.Orchestrate(context.Background(), &entity.DecisionCase{
		ID: "case-owned", ExecutionGeneration: 1,
	})
	if !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("legacy Orchestrate error = %v, want ErrLeaseLost", err)
	}
}
