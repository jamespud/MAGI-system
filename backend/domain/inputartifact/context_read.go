package inputartifact

import "github.com/jamespud/magi/backend/domain/canonical"

// ReadContextManifest checks original stored bytes without repairing them.
// Artifact-specific limits are checked before C0 Adopt's wider default limits.
func ReadContextManifest(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error) {
	if expectedID == "" {
		return ValidatedContextManifest{}, invalid("IDENTITY", "$.artifact_id", "readback requires expected identity")
	}
	if _, e := canonical.CanonicalizeWithLimits(raw, contextBodyLimits(opts.Limits)); e != nil {
		return ValidatedContextManifest{}, canonicalError(e)
	}
	c, e := canonical.Adopt(raw)
	if e != nil {
		return ValidatedContextManifest{}, canonicalError(e)
	}
	return validateContextCanonical(c, expectedID, opts)
}

// ReadContextManifestEnvelope bounds transport separately from the body. It
// retains the original nested bytes for Adopt; the wrapper is never hashed.
func ReadContextManifestEnvelope(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error) {
	env, e := readArtifactEnvelope(raw, expectedID, ValidationOptions{Limits: contextBodyLimits(opts.Limits), EnvelopeLimits: opts.EnvelopeLimits})
	if e != nil {
		return ValidatedContextManifest{}, e
	}
	v, e := ReadContextManifest(env.Body, expectedID, opts)
	if e != nil {
		return ValidatedContextManifest{}, e
	}
	if v.Digest() != env.Digest {
		return ValidatedContextManifest{}, invalid("IDENTITY", "$.digest", "wrapper digest mismatch")
	}
	return v, nil
}
