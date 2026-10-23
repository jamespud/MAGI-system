package inputartifact

import "github.com/jamespud/magi/backend/domain/canonical"

// ReadPersonaSpec refuses to repair stored bytes. Apply body limits before
// Adopt, whose C0 default ceilings must not bypass the caller's stricter caps.
func ReadPersonaSpec(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error) {
	if expectedID == "" {
		return ValidatedPersonaSpec{}, invalid("IDENTITY", "$.artifact_id", "readback requires expected identity")
	}
	if _, e := canonical.CanonicalizeWithLimits(raw, opts.Limits); e != nil {
		return ValidatedPersonaSpec{}, canonicalError(e)
	}
	c, e := canonical.Adopt(raw)
	if e != nil {
		return ValidatedPersonaSpec{}, canonicalError(e)
	}
	return validatePersonaCanonical(c, expectedID, opts)
}

// ReadPersonaSpecEnvelope bounds wrapper and body resources independently.
func ReadPersonaSpecEnvelope(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error) {
	env, e := readArtifactEnvelope(raw, expectedID, ValidationOptions{Limits: opts.Limits, EnvelopeLimits: opts.EnvelopeLimits})
	if e != nil {
		return ValidatedPersonaSpec{}, e
	}
	v, e := ReadPersonaSpec(env.Body, expectedID, opts)
	if e != nil {
		return ValidatedPersonaSpec{}, e
	}
	if v.Digest() != env.Digest {
		return ValidatedPersonaSpec{}, invalid("IDENTITY", "$.digest", "wrapper digest mismatch")
	}
	return v, nil
}
