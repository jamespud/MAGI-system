package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/application/metrics"
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

// persistArtifact's failure contract differs by authority: a durable owner
// makes every authoritative write fail-closed, while the legacy/non-durable
// path keeps the historical tolerate-and-count behaviour (ErrLeaseLost still
// escalates on both).
func TestPersistArtifactFailsClosedOnlyForDurableOwner(t *testing.T) {
	storageErr := errors.New("mysql: lock wait timeout exceeded")
	durable := &entity.ExecutionContext{
		CaseID: "case-1", JobID: "job-1", WorkerID: "worker-1", ExecutionGeneration: 4,
	}
	// Missing worker/generation: not a durable owner, so writes stay best effort.
	legacy := &entity.ExecutionContext{CaseID: "case-1", JobID: "job-1"}

	cases := []struct {
		name    string
		owner   *entity.ExecutionContext
		write   error
		want    error
		wantNil bool
	}{
		{name: "durable storage error", owner: durable, write: storageErr, want: storageErr},
		{name: "durable lease lost", owner: durable, write: port.ErrLeaseLost, want: port.ErrLeaseLost},
		{name: "legacy storage error", owner: nil, write: storageErr, wantNil: true},
		{name: "legacy lease lost", owner: nil, write: port.ErrLeaseLost, want: port.ErrLeaseLost},
		{name: "non-durable owner storage error", owner: legacy, write: storageErr, wantNil: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &Orchestrator{}
			err := o.persistArtifact(context.Background(), metrics.ArtifactAgentRun, "case-1", tc.owner, func() error {
				return tc.write
			})
			if tc.wantNil {
				if err != nil {
					t.Fatalf("persistArtifact = %v, want tolerated (nil)", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("persistArtifact = %v, want %v", err, tc.want)
			}
		})
	}
}
