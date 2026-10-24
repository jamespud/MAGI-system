package inputartifact

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"github.com/jamespud/magi/backend/domain/canonical"
	"strings"
)

// Exact frozen v2 mirror, never the v1 Context schema. Parity is a CI gate.
//
//go:embed readset_schema.json
var readSetContractSchema []byte
var readSetSchemas = func() map[string][]byte {
	var source map[string]json.RawMessage
	if e := json.Unmarshal(readSetContractSchema, &source); e != nil {
		panic(e)
	}
	delete(source, "oneOf")
	schemas := map[string][]byte{}
	for _, name := range []string{"ReadSetSnapshot", "SourceNode", "ReadEvent", "CandidateSnapshot", "TrustedDeliveryReceipt", "EvidenceClosureRecord", "CompletenessRecord", "CandidateConfirmation"} {
		source["$ref"] = json.RawMessage(`"#/$defs/` + name + `"`)
		raw, e := json.Marshal(source)
		if e != nil {
			panic(e)
		}
		schemas[name] = raw
	}
	return schemas
}()

// ValidatedReadSetSnapshot is immutable offline consistency evidence. Its zero
// value grants nothing. Accessors decode/copy rather than exposing shared data.
type ValidatedReadSetSnapshot struct {
	body       canonical.Canonical
	id, digest string
	pending    []Dependency
	status     ReadSetValidationStatus
}

func (v ValidatedReadSetSnapshot) ArtifactID() string     { return v.id }
func (v ValidatedReadSetSnapshot) Digest() string         { return v.digest }
func (v ValidatedReadSetSnapshot) CanonicalBytes() []byte { return v.body.Bytes() }
func (v ValidatedReadSetSnapshot) Body() ReadSetSnapshot {
	var b ReadSetSnapshot
	if !v.body.IsZero() {
		_ = json.Unmarshal(v.body.Bytes(), &b)
	}
	return b
}
func (v ValidatedReadSetSnapshot) Status() ReadSetValidationStatus { return v.status }
func (v ValidatedReadSetSnapshot) PendingDependencies() []Dependency {
	return append([]Dependency(nil), v.pending...)
}
func readSetLimits(l ResourceLimits) ResourceLimits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = 1048576
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = 32
	}
	if l.MaxMembers <= 0 {
		l.MaxMembers = 65536
	}
	if l.MaxStringBytes <= 0 {
		l.MaxStringBytes = 16384
	}
	return l
}
func readSetSchema(raw []byte, name string) error {
	r := schemaValidator.Validate(readSetSchemas[name], raw)
	if !r.Valid {
		v := r.Violations[0]
		return invalid("SCHEMA", "$."+v.Field, v.Message)
	}
	return nil
}
func ValidateReadSetSnapshot(raw []byte, expectedID string, o ReadSetValidationOptions) (ValidatedReadSetSnapshot, error) {
	c, e := canonical.CanonicalizeWithLimits(raw, readSetLimits(o.Limits))
	if e != nil {
		return ValidatedReadSetSnapshot{}, canonicalError(e)
	}
	return validateReadSet(c, expectedID, o)
}

// ReadReadSetSnapshot verifies original stored bytes, never silently repairs
// whitespace/escapes/order. Canonical comparison uses the caller's bounded C0
// parse, avoiding C0 Adopt's smaller default member ceiling on larger bodies.
func ReadReadSetSnapshot(raw []byte, expectedID string, o ReadSetValidationOptions) (ValidatedReadSetSnapshot, error) {
	if expectedID == "" {
		return ValidatedReadSetSnapshot{}, invalid("IDENTITY", "$", "readback requires expected identity")
	}
	c, e := canonical.CanonicalizeWithLimits(raw, readSetLimits(o.Limits))
	if e != nil {
		return ValidatedReadSetSnapshot{}, canonicalError(e)
	}
	if !bytes.Equal(raw, c.Bytes()) {
		return ValidatedReadSetSnapshot{}, invalid("NON_CANONICAL", "$", "stored bytes must already be canonical")
	}
	return validateReadSet(c, expectedID, o)
}
func ReadReadSetSnapshotEnvelope(raw []byte, expectedID string, o ReadSetValidationOptions) (ValidatedReadSetSnapshot, error) {
	env, e := readArtifactEnvelope(raw, expectedID, ValidationOptions{Limits: readSetLimits(o.Limits), EnvelopeLimits: o.EnvelopeLimits})
	if e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	v, e := ReadReadSetSnapshot(env.Body, expectedID, o)
	if e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	if v.Digest() != env.Digest {
		return ValidatedReadSetSnapshot{}, invalid("IDENTITY", "$.digest", "wrapper digest mismatch")
	}
	return v, nil
}
func validateReadSet(c canonical.Canonical, expectedID string, o ReadSetValidationOptions) (ValidatedReadSetSnapshot, error) {
	if e := readSetSchema(c.Bytes(), "ReadSetSnapshot"); e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	var b ReadSetSnapshot
	_ = json.Unmarshal(c.Bytes(), &b)
	if e := validateReadSetLocal(b, o); e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	pending, e := pendingArtifactReferences(b, b.CreatedBy)
	if e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	more, status, e := checkReadSetBindings(b, o)
	if e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	pending = append(pending, more...)
	for _, d := range pending {
		if strings.HasPrefix(d.Reference.ObjectID, "rset_") {
			return ValidatedReadSetSnapshot{}, invalid("REFERENCE", d.Path, "preexisting dependencies must not reference a future ReadSet identity")
		}
	}
	if e = validateDependencyConsistency(pending); e != nil {
		return ValidatedReadSetSnapshot{}, e
	}
	if o.RequireCompleteQualification || o.RequireActualReadProof {
		return ValidatedReadSetSnapshot{}, invalid("UNVERIFIABLE_REFERENCE", "$", "offline declarations cannot prove actual provider exposure, provenance, complete Journal, qualified issuer or C2/C3 authority")
	}
	id, _ := c.ArtifactID(canonical.TypeReadSetSnapshot)
	digest, _ := c.Digest(canonical.TypeReadSetSnapshot)
	if expectedID != "" && expectedID != id {
		return ValidatedReadSetSnapshot{}, invalid("IDENTITY", "$.artifact_id", "expected rset identity differs from complete body")
	}
	return ValidatedReadSetSnapshot{body: c, id: id, digest: digest, pending: pending, status: status}, nil
}
