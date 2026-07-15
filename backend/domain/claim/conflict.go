package claim

import "github.com/jamespud/magi/backend/domain/entity"

// ConflictFinder finds conflicting claim pairs.
type ConflictFinder interface {
	Find(claims []*entity.Claim) []entity.ClaimConflict
}

// GraphConflictFinder finds conflicting claim pairs via the claim graph.
// Agent-asserted contradictions are returned directly; S3+ may add embedding-based detection.
type GraphConflictFinder struct{}

// NewNilConflictFinder is a legacy alias for NewGraphConflictFinder.
func NewNilConflictFinder() *GraphConflictFinder { return &GraphConflictFinder{} }

// NewGraphConflictFinder returns a GraphConflictFinder.
func NewGraphConflictFinder() *GraphConflictFinder { return &GraphConflictFinder{} }

func (n *GraphConflictFinder) Find(claims []*entity.Claim) []entity.ClaimConflict {
	g := NewGraph(claims)
	return g.Conflicts()
}
