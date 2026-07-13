package claim

import "github.com/jamespud/magi/backend/domain/entity"

// Graph provides read-only queries over the claim graph.
type Graph struct {
	claims map[string]*entity.Claim
}

func NewGraph(claims []*entity.Claim) *Graph {
	m := make(map[string]*entity.Claim, len(claims))
	for _, c := range claims {
		m[c.ID] = c
	}
	return &Graph{claims: m}
}

func (g *Graph) Supports(claimID string) []string {
	if c, ok := g.claims[claimID]; ok {
		return c.Supports
	}
	return nil
}

func (g *Graph) Contradicts(claimID string) []string {
	if c, ok := g.claims[claimID]; ok {
		return c.Contradicts
	}
	return nil
}

// Conflicts returns agent-asserted contradictions as ClaimConflict pairs.
func (g *Graph) Conflicts() []entity.ClaimConflict {
	var out []entity.ClaimConflict
	for _, c := range g.claims {
		for _, target := range c.Contradicts {
			out = append(out, entity.ClaimConflict{ClaimA: c.ID, ClaimB: target, Reason: "agent-asserted"})
		}
	}
	return out
}
