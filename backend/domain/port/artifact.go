package port

import "context"

// ArtifactCleaner is the legacy generation-0 cleanup hook. Generation-aware
// retries must not depend on or invoke case-wide deletion for correctness;
// T5 replaces this surface with generation-scoped retention/GC.
type ArtifactCleaner interface {
	CleanupCaseArtifacts(ctx context.Context, caseID string) error
}
