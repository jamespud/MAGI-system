package claim_test

import (
	"testing"

	"github.com/jamespud/magi/backend/domain/claim"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestGraph_ClaimsForEvidence(t *testing.T) {
	claims := []*entity.Claim{
		{ID: "C1", Statement: "a", Supports: []string{"EV-001"}},
		{ID: "C2", Statement: "b", Supports: []string{"EV-001", "EV-002"}},
		{ID: "C3", Statement: "c", Supports: []string{"EV-002"}},
	}
	g := claim.NewGraph(claims)
	got := g.ClaimsForEvidence("EV-001")
	if len(got) != 2 {
		t.Fatalf("expected 2 claims for EV-001, got %d: %v", len(got), got)
	}
}

func TestGraph_ContradictsBidirectional(t *testing.T) {
	claims := []*entity.Claim{
		{ID: "C1", Statement: "a", Contradicts: []string{"C2"}},
		{ID: "C2", Statement: "b"},
	}
	g := claim.NewGraph(claims)
	if !contains(g.Contradicts("C1"), "C2") {
		t.Fatal("Contradicts(C1) should include C2")
	}
	if !contains(g.Contradicts("C2"), "C1") {
		t.Fatal("Contradicts(C2) should include C1 (bidirectional)")
	}
}

func TestGraph_ConflictComponent(t *testing.T) {
	claims := []*entity.Claim{
		{ID: "C1", Statement: "a", Contradicts: []string{"C2"}},
		{ID: "C2", Statement: "b", Contradicts: []string{"C3"}},
		{ID: "C3", Statement: "c"},
		{ID: "C4", Statement: "isolated"},
	}
	g := claim.NewGraph(claims)
	comp := g.ConflictComponent("C1")
	if len(comp) != 3 {
		t.Fatalf("expected conflict component of 3 (C1,C2,C3), got %d: %v", len(comp), comp)
	}
	if len(g.ConflictComponent("C4")) != 1 {
		t.Fatal("isolated claim component should be just itself")
	}
}

func TestGraph_ConflictsDedup(t *testing.T) {
	claims := []*entity.Claim{
		{ID: "C1", Statement: "a", Contradicts: []string{"C2"}},
		{ID: "C2", Statement: "b", Contradicts: []string{"C1"}},
	}
	g := claim.NewGraph(claims)
	conflicts := g.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 deduplicated conflict pair, got %d: %+v", len(conflicts), conflicts)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
