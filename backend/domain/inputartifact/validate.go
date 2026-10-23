package inputartifact

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/validation"
)

// contractSchema is an exact mirror, guarded by TestDomainSchemaMirror.
//
//go:generate cp ../../../docs/contracts/input-artifacts/schema.json schema.json
//go:embed schema.json
var contractSchema []byte
var schemaValidator = validation.NewJSONSchemaValidator()

// ResourceLimits are deployment resource ceilings, not safety or budget policy.
// Zero entries use C0 defaults; C1-A section 2.5 remains provisional.
type ResourceLimits = canonical.Limits

// DefaultEnvelopeByteAllowance bounds wrapper metadata and transport whitespace
// in addition to the body ceiling. It is an implementation allowance, not an
// artifact budget or a change to the C0/C1 contracts.
const DefaultEnvelopeByteAllowance = 1024

type ValidationOptions struct {
	// Limits applies to the DomainProfile body, including envelope readback.
	Limits ResourceLimits
	// EnvelopeLimits applies only to envelope transport. Nonpositive entries
	// derive from effective body limits: bytes + 1024, depth + 1, members + 5,
	// and string bytes at least 69 (the dprf_ identity). Additions saturate at
	// max int. Positive entries override only the corresponding outer ceiling;
	// they never relax body validation. Ignored by body-only entrypoints.
	EnvelopeLimits ResourceLimits
	// RequireCompleteQualification can only make validation stricter. No resolver,
	// identity source or governance root is implemented in C1-B1.
	RequireCompleteQualification bool
	// Vocabulary is a local typed code registry, never a source of authority.
	// nil selects DefaultVocabulary. Core risk attributes cannot be overridden.
	Vocabulary *Vocabulary
}

type Error struct {
	Kind   string
	Path   string
	Detail string
	Cause  error
}

func (e *Error) Error() string                { return fmt.Sprintf("%s at %s: %s", e.Kind, e.Path, e.Detail) }
func (e *Error) Unwrap() error                { return e.Cause }
func invalid(kind, path, detail string) error { return &Error{Kind: kind, Path: path, Detail: detail} }

// Dependency describes a check this offline package has NOT performed.
// It never asserts source existence, issuer qualification, freshness or approval.
type Dependency struct {
	Kind      string
	Path      string
	Reference ExternalRef
	Detail    string
}

// ValidatedDomainProfile is immutable local validation evidence, not a release
// record. Its zero value has no identity. Accessors return independent copies.
type ValidatedDomainProfile struct {
	body       canonical.Canonical
	id, digest string
	pending    []Dependency
}

func (v ValidatedDomainProfile) ArtifactID() string     { return v.id }
func (v ValidatedDomainProfile) Digest() string         { return v.digest }
func (v ValidatedDomainProfile) CanonicalBytes() []byte { return v.body.Bytes() }
func (v ValidatedDomainProfile) PendingDependencies() []Dependency {
	return append([]Dependency(nil), v.pending...)
}
func (v ValidatedDomainProfile) Body() DomainProfile {
	var d DomainProfile
	if !v.body.IsZero() {
		_ = json.Unmarshal(v.body.Bytes(), &d)
	}
	return d
}

// ValidateDomainProfile strictly parses C0 first, then validates the existing
// structural Schema, local semantics and only then derives or checks identity.
func ValidateDomainProfile(raw []byte, expectedID string, opts ValidationOptions) (ValidatedDomainProfile, error) {
	c, e := canonical.CanonicalizeWithLimits(raw, opts.Limits)
	if e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	return validateCanonical(c, expectedID, opts)
}
func canonicalError(e error) error {
	path := "$"
	var ce *canonical.Error
	if errors.As(e, &ce) {
		path = ce.Path
	}
	return &Error{Kind: "CANONICAL", Path: path, Detail: e.Error(), Cause: e}
}
func validateCanonical(c canonical.Canonical, expectedID string, opts ValidationOptions) (ValidatedDomainProfile, error) {
	raw := c.Bytes()
	res := schemaValidator.Validate(contractSchema, raw)
	if !res.Valid {
		v := res.Violations[0]
		return ValidatedDomainProfile{}, invalid("SCHEMA", "$."+v.Field, v.Message)
	}
	var d DomainProfile
	if e := json.Unmarshal(raw, &d); e != nil {
		return ValidatedDomainProfile{}, invalid("SCHEMA", "$", e.Error())
	}
	if d.ArtifactType != "domain_profile" {
		return ValidatedDomainProfile{}, invalid("TYPE", "$.artifact_type", "expected domain_profile")
	}
	pending, e := validateDomain(d, opts)
	if e != nil {
		return ValidatedDomainProfile{}, e
	}
	if opts.RequireCompleteQualification && len(pending) > 0 {
		return ValidatedDomainProfile{}, invalid("UNVERIFIABLE_REFERENCE", pending[0].Path, "complete qualification unavailable; C2 required")
	}
	id, e := c.ArtifactID(canonical.TypeDomainProfile)
	if e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	digest, e := c.Digest(canonical.TypeDomainProfile)
	if e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	if expectedID != "" && expectedID != id {
		return ValidatedDomainProfile{}, invalid("IDENTITY", "$.artifact_id", "expected identity does not match domain_profile content")
	}
	return ValidatedDomainProfile{body: c, id: id, digest: digest, pending: pending}, nil
}

// ReadDomainProfile refuses stored noncanonical bytes; it never repairs them.
func ReadDomainProfile(raw []byte, expectedID string, opts ValidationOptions) (ValidatedDomainProfile, error) {
	if expectedID == "" {
		return ValidatedDomainProfile{}, invalid("IDENTITY", "$.artifact_id", "readback requires expected identity")
	}
	if _, e := canonical.CanonicalizeWithLimits(raw, opts.Limits); e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	c, e := canonical.Adopt(raw)
	if e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	return validateCanonical(c, expectedID, opts)
}

type Envelope struct {
	ArtifactID       string          `json:"artifact_id"`
	Digest           string          `json:"digest"`
	CanonicalProfile string          `json:"canonical_profile"`
	DigestAlg        string          `json:"digest_alg"`
	Body             json.RawMessage `json:"body"`
}

// ReadDomainProfileEnvelope checks both transport metadata and the original
// nested body bytes, including canonicality. The wrapper itself is not hashed.
func ReadDomainProfileEnvelope(raw []byte, expectedID string, opts ValidationOptions) (ValidatedDomainProfile, error) {
	if _, e := canonical.CanonicalizeWithLimits(raw, domainEnvelopeLimits(opts)); e != nil {
		return ValidatedDomainProfile{}, canonicalError(e)
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(raw, &fields); e != nil || len(fields) != 5 {
		return ValidatedDomainProfile{}, invalid("ENVELOPE", "$", "exact five wrapper fields required")
	}
	for _, k := range []string{"artifact_id", "digest", "canonical_profile", "digest_alg", "body"} {
		if _, ok := fields[k]; !ok || bytes.Equal(fields[k], []byte("null")) {
			return ValidatedDomainProfile{}, invalid("ENVELOPE", "$."+k, "required nonnull wrapper field")
		}
	}
	var env Envelope
	if e := json.Unmarshal(raw, &env); e != nil {
		return ValidatedDomainProfile{}, invalid("ENVELOPE", "$", e.Error())
	}
	if env.CanonicalProfile != canonical.Profile || env.DigestAlg != canonical.DigestAlg || env.ArtifactID != expectedID {
		return ValidatedDomainProfile{}, invalid("ENVELOPE", "$", "wrapper identity/profile/algorithm mismatch")
	}
	v, e := ReadDomainProfile(env.Body, expectedID, opts)
	if e != nil {
		return ValidatedDomainProfile{}, e
	}
	if v.Digest() != env.Digest {
		return ValidatedDomainProfile{}, invalid("IDENTITY", "$.digest", "wrapper digest mismatch")
	}
	return v, nil
}

// domainEnvelopeLimits keeps transport parsing bounded without charging the
// wrapper against the body's own resources. C0 itself remains unchanged.
func domainEnvelopeLimits(opts ValidationOptions) ResourceLimits {
	defaults := canonical.DefaultLimits()
	effective := func(value, fallback int) int {
		if value > 0 {
			return value
		}
		return fallback
	}
	add := func(value, extra int) int {
		maxInt := int(^uint(0) >> 1)
		if value > maxInt-extra {
			return maxInt
		}
		return value + extra
	}
	body := opts.Limits
	outer := opts.EnvelopeLimits
	return ResourceLimits{
		MaxBytes:       effective(outer.MaxBytes, add(effective(body.MaxBytes, defaults.MaxBytes), DefaultEnvelopeByteAllowance)),
		MaxDepth:       effective(outer.MaxDepth, add(effective(body.MaxDepth, defaults.MaxDepth), 1)),
		MaxMembers:     effective(outer.MaxMembers, add(effective(body.MaxMembers, defaults.MaxMembers), 5)),
		MaxStringBytes: effective(outer.MaxStringBytes, max(effective(body.MaxStringBytes, defaults.MaxStringBytes), 69)),
	}
}
