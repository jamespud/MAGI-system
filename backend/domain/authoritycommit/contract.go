package authoritycommit

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/canonical"
)

var (
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9_./:-]{1,256}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// The frozen v2 projection is ordered and closed: no arbitrary write labels.
var admissionMutations = [...]string{
	"admission", "dependency_edges", "exposure_fence", "journal_event",
	"journal_high_watermark", "lineage", "outbox", "pending_transition",
	"provider_reservation", "token_consumed", "unique_binding",
}

// Validate checks only the exact request contract. It does not verify a proof,
// allocate a sequence, consume a token, or create a commit or exposure permit.
func Validate(req AdmissionRequest) Result {
	if req.ContractVersion != ContractVersion {
		return denied("CONTRACT_VERSION_INVALID")
	}
	if req.Operation != JournalAdmitV2 || req.Verification.Operation != JournalAdmitV2 {
		return denied("OPERATION_INVALID")
	}
	v, a := req.Verification, req.Admission
	if !strings.HasPrefix(v.ProofID, "aprf_") || !digestPattern.MatchString(strings.TrimPrefix(v.ProofID, "aprf_")) {
		return denied("PROOF_ID_INVALID")
	}
	if strings.TrimSpace(req.CommitToken) == "" {
		return denied("COMMIT_TOKEN_REQUIRED")
	}
	if a.Principal != v.Subject || !validSubject(a.Principal) {
		return denied("PRINCIPAL_MISMATCH")
	}
	if !idPattern.MatchString(req.ExactTargetID) || req.ExactTargetID != a.AdmissionInput.ID || v.Resource != req.ExactTargetID {
		return denied("TARGET_MISMATCH")
	}
	if a.Key.CaseID != v.CaseID || !idPattern.MatchString(a.Key.CaseID) {
		return denied("CASE_MISMATCH")
	}
	if a.Key.Round != v.Round || a.Key.Round == 0 {
		return denied("ROUND_MISMATCH")
	}
	if a.ExecutionGeneration == 0 || a.ExecutionGeneration != v.ExecutionGeneration || a.Fence.ExecutionGeneration != a.ExecutionGeneration {
		return denied("GENERATION_MISMATCH")
	}
	if !validScopes(a.ScopedEpochs) || !validScopes(v.Scopes) || !validScopes(a.Fence.ScopedEpochs) ||
		!slices.Equal(a.ScopedEpochs, v.Scopes) || !slices.Equal(a.ScopedEpochs, a.Fence.ScopedEpochs) {
		return denied("SCOPE_DRIFT")
	}
	if slices.Contains(a.MutationSet, "exposure_permit") {
		return denied("EXPOSURE_PERMIT_FORBIDDEN")
	}
	if !slices.Equal(a.MutationSet, admissionMutations[:]) {
		return denied("MUTATION_SET_INVALID")
	}
	hwm := a.HighWatermark
	// Test the upper bound before addition, so uint64 overflow cannot look valid.
	if !idPattern.MatchString(hwm.JournalID) || hwm.Expected >= uint64(canonical.MaxSafeInteger) || hwm.Next != hwm.Expected+1 {
		return denied("HWM_TRANSITION_INVALID")
	}
	pending := a.PendingTransition
	if pending.JournalID != hwm.JournalID || pending.Expected >= 512 || pending.Next != pending.Expected+1 {
		return denied("PENDING_TRANSITION_INVALID")
	}
	reservation := a.ProviderReservation
	if reservation.RequestID != a.Key.ProviderRequestID {
		return denied("TARGET_MISMATCH")
	}
	if reservation.ExpectedStatus != "ABSENT" || reservation.NextStatus != "RESERVED" ||
		!idPattern.MatchString(reservation.RequestID) || !idPattern.MatchString(reservation.SessionScopeID) {
		return denied("RESERVATION_TRANSITION_INVALID")
	}
	if len(a.DependencyEdgeInputs) == 0 || len(a.DependencyEdgeInputs) > 64 {
		return denied("DEPENDENCY_INVALID")
	}
	for _, object := range append([]PreparedObject{a.AdmissionInput, a.JournalEventInput, a.Fence.ProviderCapabilityRef}, a.DependencyEdgeInputs...) {
		if !idPattern.MatchString(object.ID) || !idPattern.MatchString(object.ObjectRef) {
			return denied("DEPENDENCY_INVALID")
		}
		if !digestPattern.MatchString(object.Digest) {
			return denied("DIGEST_INVALID")
		}
	}
	fence := a.Fence
	if (fence.Mode != "PROVIDER_ENFORCED_LEASE" && fence.Mode != "AUTHORITY_ORDERED_GATE") ||
		!idPattern.MatchString(fence.LeaseID) || !validUTCMillis(fence.LeaseUntil) {
		return denied("FENCE_INVALID")
	}
	if !idPattern.MatchString(v.TrustDomain) || !idPattern.MatchString(v.Action) || !idPattern.MatchString(v.Tenant) ||
		!idPattern.MatchString(a.Key.ContextManifestID) || !idPattern.MatchString(a.Key.DeliveryID) ||
		(a.Key.PersonaID != "melchior" && a.Key.PersonaID != "balthasar" && a.Key.PersonaID != "casper") {
		return denied("REQUEST_INVALID")
	}
	// Apply C0's NFC, safe-integer, UTF-8 and resource ceilings to every field,
	// including request metadata not otherwise constrained by the frozen plan.
	if _, err := canonical.CanonicalizeStruct(req); err != nil {
		return denied("REQUEST_INVALID")
	}
	return Result{Outcome: authority.OutcomeValid, Code: "ADMISSION_REQUEST_VALID"}
}

func denied(code string) Result {
	return Result{Outcome: authority.OutcomeDenied, Code: code}
}

func validSubject(subject authority.Subject) bool {
	return idPattern.MatchString(subject.SubjectID) && idPattern.MatchString(subject.IdentitySourceID) && idPattern.MatchString(subject.IdentitySourceVersion)
}

func validScopes(scopes []authority.ScopedEpoch) bool {
	kinds := [...]authority.ScopeKind{authority.ScopeAuthority, authority.ScopeContext, authority.ScopeCredential}
	if len(scopes) != len(kinds) {
		return false
	}
	// Exactly one of each kind also fixes the order; no scope normalization or
	// sorting may silently repair a different token-bound request.
	for i, scope := range scopes {
		if scope.Kind != kinds[i] || !idPattern.MatchString(scope.ScopeID) || scope.Epoch > uint64(canonical.MaxSafeInteger) {
			return false
		}
	}
	return true
}

func validUTCMillis(value string) bool {
	const layout = "2006-01-02T15:04:05.000Z"
	parsed, err := time.Parse(layout, value)
	return err == nil && parsed.Format(layout) == value
}

// RequestDigest hashes the entire canonical request, including CommitToken.
// The preimage is the literal v2 prefix followed by the lowercase hexadecimal
// C0 authority_proof digest. Digest calculation alone does not validate authority.
func RequestDigest(req AdmissionRequest) (string, error) {
	return digestWithPrefix(req, "magi:authority-journal-admit-v2:")
}

// ProjectAdmissionIdentity snapshots only the durable six-field identity. It
// performs no request validation, authentication, allocation or authorization.
func ProjectAdmissionIdentity(req AdmissionRequest) AdmissionIdentity {
	k := req.Admission.Key
	return AdmissionIdentity{
		trustDomain: req.Verification.TrustDomain, caseID: k.CaseID, round: k.Round,
		contextManifestID: k.ContextManifestID, personaID: k.PersonaID, deliveryID: k.DeliveryID,
	}
}

// UniqueKey derives a stable lookup identity, not a permission or request digest.
// It only checks C0 representability; even an empty tuple can be identified
// without authenticating a proof or validating an admission request.
func UniqueKey(identity AdmissionIdentity) (string, error) {
	return digestWithPrefix(map[string]any{
		"trust_domain":        identity.trustDomain,
		"case_id":             identity.caseID,
		"round":               identity.round,
		"context_manifest_id": identity.contextManifestID,
		"persona_id":          identity.personaID,
		"delivery_id":         identity.deliveryID,
	}, "magi:authority-journal-admit-v2:unique-key:")
}

// LineageDigest binds the allocation and both distinct counters to the request.
// LineageDigest itself is excluded, avoiding a self-referential digest.
func LineageDigest(commit AdmissionCommit) (string, error) {
	body := struct {
		RequestDigest        string `json:"request_digest"`
		CommitID             string `json:"commit_id"`
		AuthoritySequence    uint64 `json:"authority_sequence"`
		JournalHighWatermark uint64 `json:"journal_high_watermark"`
	}{commit.RequestDigest, commit.CommitID, commit.AuthoritySequence, commit.JournalHighWatermark}
	return digestWithPrefix(body, "magi:authority-journal-admit-v2:lineage:")
}

func digestWithPrefix(value any, prefix string) (string, error) {
	c, err := canonical.CanonicalizeStruct(value)
	if err != nil {
		return "", err
	}
	digest, err := c.Digest(canonical.TypeAuthorityProof)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(prefix + digest))
	return hex.EncodeToString(sum[:]), nil
}
