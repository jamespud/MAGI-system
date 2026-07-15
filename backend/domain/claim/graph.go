package claim

import "github.com/jamespud/magi/backend/domain/entity"

// Graph provides read-only queries over the claim graph (§11). It maintains
// a bidirectional contradiction adjacency and an evidence->claims reverse
// index, enabling reverse queries and transitive conflict traversal.
type Graph struct {
	claims           map[string]*entity.Claim
	evidenceToClaims map[string][]string
	contradictions   map[string]map[string]bool
}

func NewGraph(claims []*entity.Claim) *Graph {
	g := &Graph{
		claims:           make(map[string]*entity.Claim, len(claims)),
		evidenceToClaims: make(map[string][]string),
		contradictions:   make(map[string]map[string]bool),
	}
	for _, c := range claims {
		if c == nil {
			continue
		}
		g.claims[c.ID] = c
		for _, evID := range c.Supports {
			g.evidenceToClaims[evID] = append(g.evidenceToClaims[evID], c.ID)
		}
		for _, target := range c.Contradicts {
			g.addContradiction(c.ID, target)
		}
	}
	return g
}

func (g *Graph) addContradiction(a, b string) {
	if g.contradictions[a] == nil {
		g.contradictions[a] = make(map[string]bool)
	}
	if g.contradictions[b] == nil {
		g.contradictions[b] = make(map[string]bool)
	}
	g.contradictions[a][b] = true
	g.contradictions[b][a] = true // bidirectional
}

func (g *Graph) Supports(claimID string) []string {
	if c, ok := g.claims[claimID]; ok {
		return c.Supports
	}
	return nil
}

// Contradicts returns all claim IDs that contradict claimID (bidirectional:
// both claims it contradicts and claims that contradict it).
func (g *Graph) Contradicts(claimID string) []string {
	targets, ok := g.contradictions[claimID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(targets))
	for t := range targets {
		out = append(out, t)
	}
	return out
}

// ClaimsForEvidence returns IDs of claims that support the given EV-ID
// (reverse index).
func (g *Graph) ClaimsForEvidence(evID string) []string {
	return g.evidenceToClaims[evID]
}

// ConflictComponent returns claimID plus all claims transitively connected
// to it via contradiction edges (BFS over the contradiction adjacency).
func (g *Graph) ConflictComponent(claimID string) []string {
	visited := map[string]bool{claimID: true}
	queue := []string{claimID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range g.contradictions[cur] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	out := make([]string, 0, len(visited))
	for id := range visited {
		out = append(out, id)
	}
	return out
}

// Conflicts returns agent-asserted contradiction pairs, deduplicated so each
// bidirectional edge (A<->B) appears once.
func (g *Graph) Conflicts() []entity.ClaimConflict {
	seen := make(map[string]bool)
	var out []entity.ClaimConflict
	for a, targets := range g.contradictions {
		for b := range targets {
			key := conflictKey(a, b)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, entity.ClaimConflict{ClaimA: a, ClaimB: b, Reason: "agent-asserted"})
		}
	}
	return out
}

func conflictKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}
