package entity_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
)

// T1 (#19): the generation is an ownership epoch, so a negative value is a
// programming error rather than a state the fencing predicates have to interpret.
func TestDecisionCase_ValidateExecutionGeneration(t *testing.T) {
	positive := entity.DecisionCase{ExecutionGeneration: 3}
	if err := positive.ValidateExecutionGeneration(); err != nil {
		t.Fatalf("a positive generation must be valid: %v", err)
	}
	legacy := entity.DecisionCase{ExecutionGeneration: 0}
	if err := legacy.ValidateExecutionGeneration(); err != nil {
		t.Fatalf("generation 0 marks legacy provenance and must be valid: %v", err)
	}
	if err := (entity.DecisionCase{ExecutionGeneration: -1}).ValidateExecutionGeneration(); err == nil {
		t.Fatal("a negative generation must be rejected")
	}
}
