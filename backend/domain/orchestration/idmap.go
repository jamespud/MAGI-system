package orchestration

import "github.com/jamespud/magi/backend/domain/entity"

// ArtifactRemap is re-exported from entity so orchestrator code reads naturally
// while the shared type lives at the bottom of the dependency graph.
type ArtifactRemap = entity.ArtifactRemap

// newArtifactRemap constructs the shared remap.
func newArtifactRemap() *ArtifactRemap {
	return entity.NewArtifactRemap()
}

// mergeRemaps folds every round's remap into one, so a report that cites
// evidence from both the investigate and reconsider rounds resolves against
// the persisted artifacts regardless of which round produced the citation.
func mergeRemaps(remaps []*ArtifactRemap) *ArtifactRemap {
	out := newArtifactRemap()
	for _, r := range remaps {
		out.Merge(r)
	}
	return out
}

// remapReflection rewrites a persisted reflection's ID references to the
// namespaced artifact IDs before it is written to the DB.
func remapReflection(r *entity.Reflection, remap *ArtifactRemap) {
	if r == nil || remap == nil {
		return
	}
	r.NewEvidenceIDs = remap.RemapList(r.NewEvidenceIDs)
	r.AcceptedClaims = remap.RemapList(r.AcceptedClaims)
	r.RejectedClaims = remap.RemapList(r.RejectedClaims)
}
