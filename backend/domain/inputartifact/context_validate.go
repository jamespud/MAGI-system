package inputartifact

import (
	"encoding/json"
	"github.com/jamespud/magi/backend/domain/canonical"
)

// ValidatedContextManifest retains immutable offline evidence only. Its zero
// value has no identity/status; all returned nested data are independent copies.
type ValidatedContextManifest struct {
	body       canonical.Canonical
	id, digest string
	pending    []Dependency
	status     ContextValidationStatus
}

func (v ValidatedContextManifest) ArtifactID() string     { return v.id }
func (v ValidatedContextManifest) Digest() string         { return v.digest }
func (v ValidatedContextManifest) CanonicalBytes() []byte { return v.body.Bytes() }
func (v ValidatedContextManifest) PendingDependencies() []Dependency {
	return append([]Dependency(nil), v.pending...)
}
func (v ValidatedContextManifest) Status() ContextValidationStatus { return v.status }
func (v ValidatedContextManifest) Body() ContextManifest {
	var m ContextManifest
	if !v.body.IsZero() {
		_ = json.Unmarshal(v.body.Bytes(), &m)
	}
	return m
}

func contextBodyLimits(l ResourceLimits) ResourceLimits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = 262144
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = 24
	}
	if l.MaxMembers <= 0 {
		l.MaxMembers = 4096
	}
	if l.MaxStringBytes <= 0 {
		l.MaxStringBytes = 16384
	}
	return l
}

// ValidateContextManifest applies C0 -> frozen v1 Schema -> local semantics ->
// exact supplied bindings/unresolved dependencies -> ctxm content identity.
func ValidateContextManifest(raw []byte, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error) {
	c, e := canonical.CanonicalizeWithLimits(raw, contextBodyLimits(opts.Limits))
	if e != nil {
		return ValidatedContextManifest{}, canonicalError(e)
	}
	return validateContextCanonical(c, expectedID, opts)
}
func validateContextCanonical(c canonical.Canonical, expectedID string, opts ContextValidationOptions) (ValidatedContextManifest, error) {
	raw := c.Bytes()
	res := schemaValidator.Validate(contractSchema, raw)
	if !res.Valid {
		v := res.Violations[0]
		return ValidatedContextManifest{}, invalid("SCHEMA", "$."+v.Field, v.Message)
	}
	var m ContextManifest
	if e := json.Unmarshal(raw, &m); e != nil {
		return ValidatedContextManifest{}, invalid("SCHEMA", "$", e.Error())
	}
	if m.ArtifactType != "context_manifest" {
		return ValidatedContextManifest{}, invalid("TYPE", "$.artifact_type", "expected context_manifest")
	}
	iv, e := validateContextLocal(m)
	if e != nil {
		return ValidatedContextManifest{}, e
	}
	pending, e := pendingArtifactReferences(m, m.CreatedBy)
	if e != nil {
		return ValidatedContextManifest{}, e
	}
	deps, status, e := checkContextBindings(m, opts, iv)
	if e != nil {
		return ValidatedContextManifest{}, e
	}
	pending = append(pending, deps...)
	if e = validateDependencyConsistency(pending); e != nil {
		return ValidatedContextManifest{}, e
	}
	if opts.RequireCompleteQualification {
		return ValidatedContextManifest{}, invalid("UNVERIFIABLE_REFERENCE", "$", "external Charter approval, source/data access qualification and C2/C3 authority are unavailable")
	}
	id, e := c.ArtifactID(canonical.TypeContextManifest)
	if e != nil {
		return ValidatedContextManifest{}, canonicalError(e)
	}
	digest, e := c.Digest(canonical.TypeContextManifest)
	if e != nil {
		return ValidatedContextManifest{}, canonicalError(e)
	}
	if expectedID != "" && expectedID != id {
		return ValidatedContextManifest{}, invalid("IDENTITY", "$.artifact_id", "expected identity does not match context_manifest content")
	}
	return ValidatedContextManifest{body: c, id: id, digest: digest, pending: pending, status: status}, nil
}
