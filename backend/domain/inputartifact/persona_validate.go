package inputartifact

import (
	"encoding/json"

	"github.com/jamespud/magi/backend/domain/canonical"
)

// ValidatedPersonaSpec is immutable offline evidence. All accessors return
// independent values. A zero result has no validation status or identity.
type ValidatedPersonaSpec struct {
	body       canonical.Canonical
	id, digest string
	domainID   string
	pending    []Dependency
	status     PersonaValidationStatus
	budget     Budget
	hasBudget  bool
}

func (v ValidatedPersonaSpec) ArtifactID() string     { return v.id }
func (v ValidatedPersonaSpec) Digest() string         { return v.digest }
func (v ValidatedPersonaSpec) CanonicalBytes() []byte { return v.body.Bytes() }
func (v ValidatedPersonaSpec) PendingDependencies() []Dependency {
	return append([]Dependency(nil), v.pending...)
}
func (v ValidatedPersonaSpec) Status() PersonaValidationStatus { return v.status }

// DomainProfileID retains the caller's exact selected domain context. Consult
// Status().DomainRelations before interpreting it as locally checked. It is
// not part of the Persona body identity and grants no authority.
func (v ValidatedPersonaSpec) DomainProfileID() string { return v.domainID }
func (v ValidatedPersonaSpec) Body() PersonaSpec {
	var p PersonaSpec
	if !v.body.IsZero() {
		_ = json.Unmarshal(v.body.Bytes(), &p)
	}
	return p
}

// EffectiveBudget is the intersection of the supplied local ceilings. A false
// second result means no budget context was supplied; it never means unlimited.
func (v ValidatedPersonaSpec) EffectiveBudget() (Budget, bool) {
	b := v.budget
	if b.CostLimit != nil {
		m := *b.CostLimit
		b.CostLimit = &m
	}
	return b, v.hasBudget
}

func ValidatePersonaSpec(raw []byte, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error) {
	c, e := canonical.CanonicalizeWithLimits(raw, opts.Limits)
	if e != nil {
		return ValidatedPersonaSpec{}, canonicalError(e)
	}
	return validatePersonaCanonical(c, expectedID, opts)
}
func validatePersonaCanonical(c canonical.Canonical, expectedID string, opts PersonaValidationOptions) (ValidatedPersonaSpec, error) {
	raw := c.Bytes()
	res := schemaValidator.Validate(contractSchema, raw)
	if !res.Valid {
		v := res.Violations[0]
		return ValidatedPersonaSpec{}, invalid("SCHEMA", "$."+v.Field, v.Message)
	}
	var p PersonaSpec
	if e := json.Unmarshal(raw, &p); e != nil {
		return ValidatedPersonaSpec{}, invalid("SCHEMA", "$", e.Error())
	}
	if p.ArtifactType != "persona_spec" {
		return ValidatedPersonaSpec{}, invalid("TYPE", "$.artifact_type", "expected persona_spec")
	}
	if e := validatePersonaLocal(p); e != nil {
		return ValidatedPersonaSpec{}, e
	}
	pending, e := pendingArtifactReferences(p, p.CreatedBy)
	if e != nil {
		return ValidatedPersonaSpec{}, e
	}
	deps, domainStatus, budget, hasBudget, e := validatePersonaDomain(p, opts)
	if e != nil {
		return ValidatedPersonaSpec{}, e
	}
	pending = append(pending, deps...)
	if e = validateDependencyConsistency(pending); e != nil {
		return ValidatedPersonaSpec{}, e
	}
	if opts.RequireCompleteQualification {
		return ValidatedPersonaSpec{}, invalid("UNVERIFIABLE_REFERENCE", "$", "complete qualification unavailable; external C2 governance and behavioral evidence required")
	}
	id, e := c.ArtifactID(canonical.TypePersonaSpec)
	if e != nil {
		return ValidatedPersonaSpec{}, canonicalError(e)
	}
	digest, e := c.Digest(canonical.TypePersonaSpec)
	if e != nil {
		return ValidatedPersonaSpec{}, canonicalError(e)
	}
	if expectedID != "" && expectedID != id {
		return ValidatedPersonaSpec{}, invalid("IDENTITY", "$.artifact_id", "expected identity does not match persona_spec content")
	}
	return ValidatedPersonaSpec{
		body: c, id: id, digest: digest, domainID: opts.DomainProfileID,
		pending: pending, budget: budget, hasBudget: hasBudget,
		status: PersonaValidationStatus{
			Baseline: "VALID_OFFLINE", DomainRelations: domainStatus,
			Qualification: "UNVERIFIABLE_REFERENCE", Behavior: "NOT_RUN",
		},
	}, nil
}

// Independently valid artifacts can still contradict each other about the same
// external object/version. Missing qualification cannot hide that contradiction.
func validateDependencyConsistency(deps []Dependency) error {
	seen := map[string]string{}
	for _, dep := range deps {
		r := dep.Reference
		if r.Namespace == "" {
			continue
		}
		key := r.Namespace + "\x00" + r.ObjectID + "\x00" + r.Version
		if digest, ok := seen[key]; ok && digest != r.ContentDigest {
			return invalid("REFERENCE", dep.Path+".content_digest", "same external object/version has conflicting content digests across selected artifacts")
		}
		seen[key] = r.ContentDigest
	}
	return nil
}
