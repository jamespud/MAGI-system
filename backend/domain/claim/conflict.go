package claim

import "github.com/jamespud/magi/backend/domain/entity"

// ConflictFinder finds conflicting claim pairs. S2: NilConflictFinder returns
// only agent-asserted contradictions; S3+ adds embedding-based detection.
type ConflictFinder interface {
	Find(claims []*entity.Claim) []entity.ClaimConflict
}

type NilConflictFinder struct{}

func NewNilConflictFinder() *NilConflictFinder { return &NilConflictFinder{} }

func (n *NilConflictFinder) Find(claims []*entity.Claim) []entity.ClaimConflict {
	g := NewGraph(claims)
	return g.Conflicts()
}
