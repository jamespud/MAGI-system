package port

import "context"

// ArtifactCleaner removes the persisted artifacts of a previous execution
// attempt so a durable retry that resumes from checkpoints does not duplicate
// agent runs, evidence, claims, votes or tool calls.
type ArtifactCleaner interface {
	CleanupCaseArtifacts(ctx context.Context, caseID string) error
}
