package magi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

var authorityCommitBlockedCapabilities = []string{
	"PROVIDER_FENCE_CAPABILITY_UNVERIFIED",
	"INDEPENDENT_WITNESS_UNAVAILABLE",
}

// AuthorityCommitRepository is the default-off MySQL consumer for the first
// real authority-ordered operation, JOURNAL_ADMIT_V2. It persists an admission
// and a non-consumable blocked fence declaration; it never calls a Provider or
// creates an exposure permit.
type AuthorityCommitRepository struct {
	db                      *gorm.DB
	authority               *AuthorityRepository
	faultHook               func(string) error
	witnessVerifier         ExposureGateAttestationVerifier
	gateAttestationVerifier ExposureGateAttestationVerifier
}

func NewAuthorityCommitRepository(db *gorm.DB, cfg AuthorityRepositoryConfig) *AuthorityCommitRepository {
	return &AuthorityCommitRepository{db: db, authority: NewAuthorityRepository(db, cfg)}
}

// CommitJournalAdmissionV2 re-authenticates C2 and commits one admission under
// the common authority head. A duplicate returns only the original lineage.
func (r *AuthorityCommitRepository) CommitJournalAdmissionV2(
	ctx context.Context,
	owner *entity.ExecutionContext,
	request authoritycommit.AdmissionRequest,
) authoritycommit.Result {
	if r == nil || r.db == nil || r.authority == nil {
		return authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_UNAVAILABLE", "")
	}
	if capability := r.authority.capabilityResult(); capability != nil {
		return authorityCommitFromAuthorityResult(*capability)
	}
	if result := authoritycommit.Validate(request); result.Outcome != authority.OutcomeValid {
		return result
	}
	if owner == nil || !owner.IsDurable() || owner.CaseID != request.Admission.Key.CaseID ||
		owner.ExecutionGeneration != int64(request.Admission.ExecutionGeneration) {
		return authorityCommitFailure(authority.OutcomeDenied, "OWNER_BINDING_MISMATCH", "")
	}
	identity := authoritycommit.ProjectAdmissionIdentity(request)
	uniqueKey, err := authoritycommit.UniqueKey(identity)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "UNIQUE_KEY_INVALID", err.Error())
	}
	requestDigest, err := authoritycommit.RequestDigest(request)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	requestRaw, err := canonical.CanonicalizeStruct(request)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	admissionDigest, err := authorityCommitDigest("magi:authority-journal-admit-v2:admission:", request.Admission)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "ADMISSION_DIGEST_INVALID", err.Error())
	}
	commitID := authorityCommitID(requestDigest)
	var committed authoritycommit.Result
	var abort *authoritycommit.Result
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		injectFault := func(point string) *authoritycommit.Result {
			if err := r.authorityCommitFault(point); err != nil {
				return authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
			}
			return nil
		}
		head, result := lockAuthorityHead(tx, request.Verification.TrustDomain)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		binding, result := lockAuthorityUniqueBinding(tx, request.Verification.TrustDomain, uniqueKey)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if binding != nil {
			if binding.RequestDigest != requestDigest || binding.ExactTargetID != request.ExactTargetID || binding.CommitToken != request.CommitToken {
				abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeConflict, "ADMISSION_KEY_CONFLICT", ""))
				return errAuthorityCommitAbort
			}
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeDenied, "AUDIT_AUTHORIZATION_REQUIRED", ""))
			return errAuthorityCommitAbort
		}
		token, result := lockAuthorityCommitToken(tx, request.Verification.TrustDomain, request.CommitToken)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if token != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeConflict, "COMMIT_TOKEN_REUSED", ""))
			return errAuthorityCommitAbort
		}
		if result := r.authority.verifyProofInTx(tx, request.Verification); result.Outcome != authority.OutcomeValid {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		job, caseModel, err := lockActiveExecution(tx, owner)
		if err != nil {
			code := authorityOwnerFailureCode(tx, owner, err)
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeStale, code, err.Error()))
			return errAuthorityCommitAbort
		}
		if job.LeaseUntil == nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeStale, "OWNER_LEASE_LOST", ""))
			return errAuthorityCommitAbort
		}

		pendingBefore, pendingAfter, journalID, result := lockOrCreateAuthorityJournalV2(tx, request, owner)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_journal"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		// The journal lock may wait behind another writer. Re-read C2 and its
		// trusted time after that wait, immediately before sequence allocation.
		if result := r.authority.verifyProofInTx(tx, request.Verification); result.Outcome != authority.OutcomeValid {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		trustedNow, err := databaseNow(tx)
		if err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if err := validateActiveExecutionAt(job, caseModel, owner, trustedNow); err != nil {
			code := classifyOwnerValidationFailure(job, caseModel, owner, trustedNow)
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeStale, code, err.Error()))
			return errAuthorityCommitAbort
		}
		authoritySequence, result := advanceAdmissionAuthorityHead(tx, head, owner, job, caseModel, request.Admission.ExecutionGeneration)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_authority_sequence"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}

		commit := authoritycommit.AdmissionCommit{
			UniqueKey: uniqueKey, RequestDigest: requestDigest, AdmissionDigest: admissionDigest,
			CommitID: commitID, ExactTargetID: request.ExactTargetID, CommitToken: request.CommitToken,
			ProofID: request.Verification.ProofID, JournalID: journalID,
			AuthoritySequence: authoritySequence, JournalLocalSequence: request.Admission.HighWatermark.Next,
			JournalHighWatermark: request.Admission.HighWatermark.Next,
			PendingBefore:        pendingBefore, PendingAfter: pendingAfter,
			CommittedAt: authorityFormatTime(trustedNow), ExposurePermitted: false,
		}
		lineageDigest, err := authoritycommit.LineageDigest(commit)
		if err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeDenied, "LINEAGE_DIGEST_INVALID", err.Error()))
			return errAuthorityCommitAbort
		}
		commit.LineageDigest = lineageDigest
		scopesRaw, err := canonical.CanonicalizeStruct(request.Admission.ScopedEpochs)
		if err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeDenied, "SCOPE_BYTES_INVALID", err.Error()))
			return errAuthorityCommitAbort
		}
		lineageRaw, err := canonical.CanonicalizeStruct(commit)
		if err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeDenied, "LINEAGE_BYTES_INVALID", err.Error()))
			return errAuthorityCommitAbort
		}
		outboxRaw, err := canonical.CanonicalizeStruct(struct {
			CommitID string `json:"commit_id"`
			Kind     string `json:"kind"`
			State    string `json:"state"`
		}{commit.CommitID, authoritycommit.JournalAdmitV2, "BLOCKED"})
		if err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeDenied, "OUTBOX_BYTES_INVALID", err.Error()))
			return errAuthorityCommitAbort
		}

		if err := insertAuthorityAdmissionV2(tx, request, owner, *job.LeaseUntil, commit, requestRaw.Bytes(), scopesRaw.Bytes()); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_admission"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityJournalEventV2(tx, request, commit); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_journal_event"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		for ordinal, dependency := range request.Admission.DependencyEdgeInputs {
			if err := insertAuthorityDependencyV2(tx, request.Verification.TrustDomain, commit, dependency, ordinal); err != nil {
				abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
				return errAuthorityCommitAbort
			}
		}
		if fault := injectFault("after_dependencies"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityReservationV2(tx, request, commit); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_reservation"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityFenceV2(tx, request, commit, scopesRaw.Bytes()); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_fence"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityPendingTransitionV2(tx, request, commit); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_pending"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityUniqueBindingV2(tx, request, commit); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_unique"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityTokenConsumedV2(tx, request.Verification.TrustDomain, commit); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_token"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityLineageV2(tx, request.Verification.TrustDomain, commit, lineageRaw.Bytes()); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_lineage"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		if err := insertAuthorityOutboxV2(tx, request.Verification.TrustDomain, commit, outboxRaw.Bytes()); err != nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if fault := injectFault("after_outbox"); fault != nil {
			abort = fault
			return errAuthorityCommitAbort
		}
		committed = authoritycommit.Result{
			Outcome: authority.OutcomeValid, Code: "ADMITTED", Disposition: "NEW",
			Commit: &commit, BlockedCapabilities: append([]string(nil), authorityCommitBlockedCapabilities...),
			ExposurePermitted: false,
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if abort != nil {
		return *abort
	}
	if err != nil {
		return authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_COMMIT_UNKNOWN", err.Error())
	}
	if err := r.authorityCommitFault("after_commit"); err != nil {
		return authorityCommitFailure(authority.OutcomeUnknown, "COMMIT_RESPONSE_LOST", err.Error())
	}
	return committed
}

// ReplayJournalAdmissionV2 reads one historical admission lineage only after a
// current, independent JOURNAL_AUDIT_READ proof succeeds. It does not require
// the original execution owner or lease and never creates a new commit.
// CommitJournalExposureGateV2 is the default-off Gate authority core. It
// allocates a gate sequence only after all local authority predicates pass.
// It never calls a provider and the returned attempt is never a consumable
// permit; a future provider fence consumer must recheck revocation at APPLY.
func (r *AuthorityCommitRepository) CommitJournalExposureGateV2(
	ctx context.Context,
	owner *entity.ExecutionContext,
	request authoritycommit.ExposureGateRequest,
) authoritycommit.ExposureGateResult {
	if r == nil || r.db == nil || r.authority == nil {
		return exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_UNAVAILABLE", "")
	}
	if capability := r.authority.capabilityResult(); capability != nil {
		return exposureGateFailure(capability.Outcome, capability.Code, capability.Detail)
	}
	if result := authoritycommit.ValidateExposureGate(request); result.Outcome != authority.OutcomeValid {
		return exposureGateFailure(result.Outcome, result.Code, result.Detail)
	}
	if owner == nil || !owner.IsDurable() || owner.CaseID != request.Verification.CaseID ||
		owner.ExecutionGeneration != int64(request.Verification.ExecutionGeneration) {
		code := "OWNER_BINDING_MISMATCH"
		outcome := authority.OutcomeDenied
		if owner != nil && owner.IsDurable() && owner.ExecutionGeneration != int64(request.Verification.ExecutionGeneration) {
			code = "EXECUTION_GENERATION_STALE"
			outcome = authority.OutcomeStale
		}
		return exposureGateFailure(outcome, code, "")
	}
	requestRaw, err := canonical.CanonicalizeStruct(request)
	if err != nil {
		return exposureGateFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	requestDigest, err := authorityCommitDigest("magi:authority-journal-exposure-gate-v2:", request)
	if err != nil {
		return exposureGateFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	gateCommitID := exposureGateCommitID(requestDigest)
	if r.gateAttestationVerifier == nil || r.witnessVerifier == nil ||
		authoritycommit.ValidateProviderCapability(request.Gate.ProviderCapability).Outcome != authority.OutcomeValid ||
		!exposureWitnessDeclared(request.Witness) {
		return r.commitBlockedExposureGate(ctx, owner, request, requestDigest)
	}

	var committed authoritycommit.ExposureGateResult
	var abort *authoritycommit.ExposureGateResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.authorityCommitFault("gate_before_head"); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
			return errAuthorityCommitAbort
		}
		head, result := lockAuthorityHead(tx, request.Verification.TrustDomain)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if err := r.authorityCommitFault("gate_after_head"); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
			return errAuthorityCommitAbort
		}
		existing, result := lockExposureGateAttempt(tx, request.Verification.TrustDomain, request.AttemptID)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if existing != nil {
			if !bytes.Equal(existing.RawRequest, requestRaw.Bytes()) || existing.RequestDigest != requestDigest ||
				existing.CommitToken != request.CommitToken {
				abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "EXPOSURE_GATE_ATTEMPT_CONFLICT", ""))
				return errAuthorityCommitAbort
			}
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeDenied, "AUDIT_AUTHORIZATION_REQUIRED", ""))
			return errAuthorityCommitAbort
		}
		tokenAttempt, result := lockExposureGateAttemptByToken(tx, request.Verification.TrustDomain, request.CommitToken)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if tokenAttempt != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "COMMIT_TOKEN_REUSED", ""))
			return errAuthorityCommitAbort
		}
		token, result := lockExposureGateToken(tx, request.Verification.TrustDomain, request.CommitToken)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if token != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "COMMIT_TOKEN_REUSED", ""))
			return errAuthorityCommitAbort
		}
		if result := r.authority.verifyProofInTx(tx, request.Verification); result.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		job, caseModel, lockErr := lockActiveExecution(tx, owner)
		if lockErr != nil {
			code := authorityOwnerFailureCode(tx, owner, lockErr)
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeStale, code, lockErr.Error()))
			return errAuthorityCommitAbort
		}
		if result := r.authority.verifyProofInTx(tx, request.Verification); result.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		admission, fence, result := lockExposureGateAdmission(tx, request)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if result := validateExposureGateAdmissionBinding(admission, fence, request, owner); result.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if admission.State != "ADMITTED" {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "ADMISSION_NOT_GATED", ""))
			return errAuthorityCommitAbort
		}
		if err := r.authorityCommitFault("gate_after_admission"); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
			return errAuthorityCommitAbort
		}
		trustedNow, err := databaseNow(tx)
		if err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		// Admission and fence locks may have waited across the proof deadline.
		// Re-authenticate C2 against fresh DB time before the final owner/fence
		// checks and before allocating any common authority sequence.
		if result := r.authority.verifyProofInTx(tx, request.Verification); result.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if err := validateActiveExecutionAt(job, caseModel, owner, trustedNow); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeStale, classifyOwnerValidationFailure(job, caseModel, owner, trustedNow), err.Error()))
			return errAuthorityCommitAbort
		}
		if !mustAuthorityTime(fence.LeaseUntilString).After(trustedNow) {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeExpired, "FENCE_EXPIRED", ""))
			return errAuthorityCommitAbort
		}
		capability := request.Gate.ProviderCapability
		if capability.CapabilityID != fence.ProviderCapabilityID ||
			capability.VerificationRef.Digest != fence.ProviderCapabilityDigest ||
			capability.VerificationRef.ObjectRef != fence.ProviderCapabilityRef {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "PROVIDER_CAPABILITY_BINDING_MISMATCH", ""))
			return errAuthorityCommitAbort
		}
		if !mustAuthorityTime(capability.MaxLeaseUntil).After(trustedNow) {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeExpired, "PROVIDER_CAPABILITY_EXPIRED", ""))
			return errAuthorityCommitAbort
		}
		if mustAuthorityTime(fence.LeaseUntilString).After(mustAuthorityTime(capability.MaxLeaseUntil)) {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeBlocked, "PROVIDER_CAPABILITY_LEASE_SHORTER_THAN_FENCE", ""))
			return errAuthorityCommitAbort
		}
		verified := r.gateAttestationVerifier.VerifyGateAttestation(request, capability, AuthorityExposureGateFenceEvidence{
			LeaseUntil: mustAuthorityTime(fence.LeaseUntilString), ProviderCapabilityID: fence.ProviderCapabilityID,
			ProviderCapabilityDigest: fence.ProviderCapabilityDigest, ProviderCapabilityRef: fence.ProviderCapabilityRef,
			ExecutionGeneration: fence.ExecutionGeneration,
		}, trustedNow)
		if verified.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFailure(verified.Outcome, verified.Code, verified.Detail))
			return errAuthorityCommitAbort
		}
		if request.Witness.RequestDigest == "" || !mustAuthorityTime(request.Witness.ValidUntil).After(trustedNow) {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeStale, "WITNESS_EVIDENCE_EXPIRED_OR_UNBOUND", ""))
			return errAuthorityCommitAbort
		}
		sequence, result := advanceExposureGateAuthorityHead(tx, head, owner, job, caseModel)
		if result.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if err := r.authorityCommitFault("gate_after_sequence"); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
			return errAuthorityCommitAbort
		}
		attempt := authoritycommit.ExposureGateAttempt{
			AttemptID: request.AttemptID, RequestDigest: requestDigest,
			Outcome: "EXPOSURE_GATED", Code: "EXPOSURE_GATE_COMMITTED",
			GateID: request.Gate.GateID, AdmissionCommitID: request.Gate.AdmissionCommitID,
			AuthoritySequence: sequence, CommittedAt: authorityFormatTime(trustedNow),
			ExposurePermitted: false,
		}
		for _, step := range []struct {
			point string
			write func() error
		}{
			{"gate_write_attempt", func() error {
				return insertExposureGateAttemptV2(tx, request, requestDigest, gateCommitID, sequence, trustedNow)
			}},
			{"gate_write_binding", func() error {
				return insertExposureGateBindingV2(tx, request, requestDigest, gateCommitID, sequence)
			}},
			{"gate_write_token", func() error {
				return insertExposureGateTokenV2(tx, request, requestDigest, gateCommitID)
			}},
			{"gate_write_lineage", func() error {
				return insertExposureGateLineageV2(tx, request, requestDigest, gateCommitID, sequence, requestRaw.Bytes())
			}},
			{"gate_write_attestation", func() error {
				return insertExposureGateAttestationV2(tx, request, gateCommitID)
			}},
		} {
			if err := step.write(); err != nil {
				abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
				return errAuthorityCommitAbort
			}
			if err := r.authorityCommitFault(step.point); err != nil {
				abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT", err.Error()))
				return errAuthorityCommitAbort
			}
		}
		committed = authoritycommit.ExposureGateResult{
			Outcome: authority.OutcomeValid, Code: "EXPOSURE_GATED", Disposition: "NEW", Attempt: &attempt,
			BlockedCapabilities: []string{"PROVIDER_FENCE_CAPABILITY_UNVERIFIED", "INDEPENDENT_WITNESS_UNAVAILABLE"},
			ExposurePermitted:   false,
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if abort != nil {
		return *abort
	}
	if err != nil {
		return exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error())
	}
	if err := r.authorityCommitFault("gate_after_commit"); err != nil {
		return exposureGateFailure(authority.OutcomeUnknown, "INJECTED_FAULT_AFTER_COMMIT", err.Error())
	}
	return committed
}

func (r *AuthorityCommitRepository) ReplayJournalAdmissionV2(ctx context.Context, request authoritycommit.ReplayRequest) authoritycommit.Result {
	if r == nil || r.db == nil || r.authority == nil {
		return authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_UNAVAILABLE", "")
	}
	if capability := r.authority.capabilityResult(); capability != nil {
		return authorityCommitFromAuthorityResult(*capability)
	}
	if result := authoritycommit.ValidateReplay(request); result.Outcome != authority.OutcomeValid {
		return result
	}
	identity := authoritycommit.ProjectAdmissionIdentity(request.Original)
	uniqueKey, err := authoritycommit.UniqueKey(identity)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "UNIQUE_KEY_INVALID", err.Error())
	}
	requestDigest, err := authoritycommit.RequestDigest(request.Original)
	if err != nil {
		return authorityCommitFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	var replayed authoritycommit.Result
	var abort *authoritycommit.Result
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, result := lockAuthorityHead(tx, request.AuditVerification.TrustDomain); result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if result := r.authority.verifyProofInTx(tx, request.AuditVerification); result.Outcome != authority.OutcomeValid {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		binding, result := lockAuthorityUniqueBinding(tx, request.AuditVerification.TrustDomain, uniqueKey)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if binding == nil {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeUnknown, "ADMISSION_NOT_FOUND", ""))
			return errAuthorityCommitAbort
		}
		if binding.RequestDigest != requestDigest || binding.ExactTargetID != request.Original.ExactTargetID ||
			binding.CommitToken != request.Original.CommitToken {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeConflict, "ADMISSION_KEY_CONFLICT", ""))
			return errAuthorityCommitAbort
		}
		original, result := readAuthorityAdmissionCommit(tx, request.AuditVerification.TrustDomain, binding.CommitID)
		if result.Outcome != "" {
			abort = authorityCommitResultPtr(authorityCommitFromAuthorityResult(result))
			return errAuthorityCommitAbort
		}
		if original.Commit == nil || original.Commit.UniqueKey != binding.UniqueKey || original.Commit.CommitID != binding.CommitID ||
			original.Commit.RequestDigest != binding.RequestDigest || original.Commit.ExactTargetID != binding.ExactTargetID ||
			original.Commit.CommitToken != binding.CommitToken || original.Commit.AuthoritySequence != binding.AuthoritySequence ||
			original.Commit.JournalHighWatermark != binding.JournalHighWatermark {
			abort = authorityCommitResultPtr(authorityCommitFailure(authority.OutcomeConflict, "ADMISSION_LINEAGE_CONFLICT", ""))
			return errAuthorityCommitAbort
		}
		replayed = original
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if abort != nil {
		return *abort
	}
	if err != nil {
		return authorityCommitFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error())
	}
	return replayed
}

var errAuthorityCommitAbort = errors.New("authority commit aborted with typed result")

func authorityCommitFailure(outcome authority.Outcome, code, detail string) authoritycommit.Result {
	return authoritycommit.Result{Outcome: outcome, Code: code, Detail: detail, ExposurePermitted: false}
}

func authorityCommitResultPtr(result authoritycommit.Result) *authoritycommit.Result { return &result }

func (r *AuthorityCommitRepository) authorityCommitFault(point string) error {
	if r == nil || r.faultHook == nil {
		return nil
	}
	return r.faultHook(point)
}

func authorityOwnerFailureCode(tx *gorm.DB, owner *entity.ExecutionContext, cause error) string {
	if !errors.Is(cause, port.ErrLeaseLost) {
		return "AUTHORITY_STORE_ERROR"
	}
	job, caseModel, err := lockExecutionRows(tx, owner.JobID, owner.CaseID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "OWNER_LEASE_LOST"
		}
		return "AUTHORITY_STORE_ERROR"
	}
	now, err := databaseNow(tx)
	if err != nil {
		return "AUTHORITY_STORE_ERROR"
	}
	return classifyOwnerValidationFailure(job, caseModel, owner, now)
}

func classifyOwnerValidationFailure(job *DecisionJobModel, caseModel *CaseModel, owner *entity.ExecutionContext, now time.Time) string {
	if job == nil || caseModel == nil || owner == nil {
		return "OWNER_LEASE_LOST"
	}
	if job.ExecutionGeneration != owner.ExecutionGeneration || caseModel.ExecutionGeneration != owner.ExecutionGeneration {
		return "EXECUTION_GENERATION_STALE"
	}
	if job.WorkerID != owner.WorkerID {
		return "OWNER_FENCE_STALE"
	}
	if job.Status != string(entity.DecisionJobRunning) {
		return "OWNER_NOT_RUNNING"
	}
	if job.LeaseUntil == nil || !job.LeaseUntil.After(now) {
		return "OWNER_LEASE_EXPIRED"
	}
	return "OWNER_LEASE_LOST"
}

func authorityCommitFromAuthorityResult(result authority.Result) authoritycommit.Result {
	return authoritycommit.Result{
		Outcome: result.Outcome, Code: result.Code, Detail: result.Detail,
		ExposurePermitted: false,
	}
}

func authorityCommitDigest(prefix string, value any) (string, error) {
	canonicalValue, err := canonical.CanonicalizeStruct(value)
	if err != nil {
		return "", err
	}
	digest, err := canonicalValue.Digest(canonical.TypeAuthorityProof)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(prefix + digest))
	return hex.EncodeToString(sum[:]), nil
}

func authorityCommitID(requestDigest string) string {
	sum := sha256.Sum256([]byte("magi:authority-journal-admit-v2:commit:" + requestDigest))
	return "ajc_" + hex.EncodeToString(sum[:])
}

func lockAuthorityUniqueBinding(tx *gorm.DB, trustDomain, uniqueKey string) (*authorityUniqueBindingV2Row, authority.Result) {
	var row authorityUniqueBindingV2Row
	err := tx.Raw(`SELECT trust_domain, unique_key, commit_id, request_digest, exact_target_id,
			commit_token, authority_sequence, journal_high_watermark, execution_generation, created_at
		FROM magi_authority_unique_binding_v2
		WHERE trust_domain = ? AND unique_key = ? FOR UPDATE`, trustDomain, uniqueKey).
		Row().Scan(&row.TrustDomain, &row.UniqueKey, &row.CommitID, &row.RequestDigest,
		&row.ExactTargetID, &row.CommitToken, &row.AuthoritySequence,
		&row.JournalHighWatermark, &row.ExecutionGeneration, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func lockAuthorityCommitToken(tx *gorm.DB, trustDomain, commitToken string) (*authorityTokenConsumedV2Row, authority.Result) {
	var row authorityTokenConsumedV2Row
	err := tx.Raw(`SELECT trust_domain, commit_token, commit_id, request_digest, consumed_at
		FROM magi_authority_token_consumed_v2
		WHERE trust_domain = ? AND commit_token = ? FOR UPDATE`, trustDomain, commitToken).
		Row().Scan(&row.TrustDomain, &row.CommitToken, &row.CommitID, &row.RequestDigest, &row.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func readAuthorityAdmissionCommit(tx *gorm.DB, trustDomain, commitID string) (authoritycommit.Result, authority.Result) {
	var row struct {
		UniqueKey, RequestDigest, AdmissionDigest, CommitID, ExactTargetID, CommitToken, ProofID, JournalID string
		AuthoritySequence, JournalHighWatermark, PendingBefore, PendingAfter                                uint64
		CommittedAt                                                                                         time.Time
	}
	err := tx.Raw(`SELECT unique_key, request_digest, admission_digest, commit_id, exact_target_id,
			commit_token, proof_id, journal_id, authority_sequence, journal_high_watermark,
			(SELECT expected_pending FROM magi_authority_pending_transition_v2 p
			 WHERE p.trust_domain = a.trust_domain AND p.commit_id = a.commit_id) AS pending_before,
			(SELECT next_pending FROM magi_authority_pending_transition_v2 p
			 WHERE p.trust_domain = a.trust_domain AND p.commit_id = a.commit_id) AS pending_after,
			committed_at
		FROM magi_authority_admission_v2 a
		WHERE trust_domain = ? AND commit_id = ?`, trustDomain, commitID).Row().Scan(
		&row.UniqueKey, &row.RequestDigest, &row.AdmissionDigest, &row.CommitID, &row.ExactTargetID,
		&row.CommitToken, &row.ProofID, &row.JournalID, &row.AuthoritySequence, &row.JournalHighWatermark,
		&row.PendingBefore, &row.PendingAfter, &row.CommittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return authoritycommit.Result{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "ADMISSION_LINEAGE_MISSING"}
	}
	if err != nil {
		return authoritycommit.Result{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	var lineage string
	if err := tx.Raw(`SELECT lineage_digest FROM magi_authority_lineage_v2 WHERE trust_domain=? AND commit_id=?`, trustDomain, commitID).Row().Scan(&lineage); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return authoritycommit.Result{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "ADMISSION_LINEAGE_MISSING"}
		}
		return authoritycommit.Result{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	commit := authoritycommit.AdmissionCommit{
		UniqueKey: row.UniqueKey, RequestDigest: row.RequestDigest, AdmissionDigest: row.AdmissionDigest,
		CommitID: row.CommitID, ExactTargetID: row.ExactTargetID, CommitToken: row.CommitToken,
		ProofID: row.ProofID, JournalID: row.JournalID, AuthoritySequence: row.AuthoritySequence,
		JournalLocalSequence: row.JournalHighWatermark, JournalHighWatermark: row.JournalHighWatermark,
		PendingBefore: row.PendingBefore, PendingAfter: row.PendingAfter,
		CommittedAt: authorityFormatTime(row.CommittedAt), LineageDigest: lineage, ExposurePermitted: false,
	}
	return authoritycommit.Result{
		Outcome: authority.OutcomeValid, Code: "ADMITTED", Disposition: "REPLAY", Commit: &commit,
		BlockedCapabilities: append([]string(nil), authorityCommitBlockedCapabilities...), ExposurePermitted: false,
	}, authority.Result{}
}

func lockOrCreateAuthorityJournalV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, owner *entity.ExecutionContext) (uint64, uint64, string, authority.Result) {
	key := request.Admission.Key
	journalID := request.Admission.HighWatermark.JournalID
	var row authorityJournalV2Row
	err := tx.Raw(`SELECT trust_domain, journal_id, case_id, case_round, context_manifest_id, persona_id,
			execution_generation, high_watermark, pending_count, row_version, state, created_at, updated_at
		FROM magi_authority_journal_v2
		WHERE trust_domain = ? AND journal_id = ? FOR UPDATE`, request.Verification.TrustDomain, journalID).Row().Scan(
		&row.TrustDomain, &row.JournalID, &row.CaseID, &row.CaseRound, &row.ContextManifestID, &row.PersonaID,
		&row.ExecutionGeneration, &row.HighWatermark, &row.PendingCount, &row.RowVersion, &row.State,
		&row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		if request.Admission.HighWatermark.Expected != 0 || request.Admission.PendingTransition.Expected != 0 {
			return 0, 0, "", authority.Result{Outcome: authority.OutcomeStale, Code: "JOURNAL_HWM_STALE"}
		}
		if err := tx.Exec(`INSERT INTO magi_authority_journal_v2
				(trust_domain, journal_id, case_id, case_round, context_manifest_id, persona_id,
				 execution_generation, high_watermark, pending_count, row_version, state)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, 'OPEN')`,
			request.Verification.TrustDomain, journalID, key.CaseID, key.Round, key.ContextManifestID,
			key.PersonaID, owner.ExecutionGeneration, request.Admission.HighWatermark.Next,
			request.Admission.PendingTransition.Next).Error; err != nil {
			return 0, 0, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
		}
		return 0, request.Admission.PendingTransition.Next, journalID, authority.Result{}
	}
	if err != nil {
		return 0, 0, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	if row.CaseID != key.CaseID || row.CaseRound != key.Round || row.ContextManifestID != key.ContextManifestID ||
		row.PersonaID != key.PersonaID || row.ExecutionGeneration != uint64(owner.ExecutionGeneration) || row.State != "OPEN" {
		return 0, 0, "", authority.Result{Outcome: authority.OutcomeConflict, Code: "JOURNAL_SCOPE_CONFLICT"}
	}
	if row.HighWatermark != request.Admission.HighWatermark.Expected || row.PendingCount != request.Admission.PendingTransition.Expected {
		return 0, 0, "", authority.Result{Outcome: authority.OutcomeStale, Code: "JOURNAL_HWM_STALE"}
	}
	updated := tx.Exec(`UPDATE magi_authority_journal_v2
		SET high_watermark = ?, pending_count = ?, row_version = row_version + 1, updated_at = CURRENT_TIMESTAMP(6)
		WHERE trust_domain = ? AND journal_id = ? AND high_watermark = ? AND pending_count = ? AND row_version = ? AND state = 'OPEN'`,
		request.Admission.HighWatermark.Next, request.Admission.PendingTransition.Next,
		request.Verification.TrustDomain, journalID, row.HighWatermark, row.PendingCount, row.RowVersion)
	if updated.Error != nil {
		return 0, 0, "", authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: updated.Error.Error()}
	}
	if updated.RowsAffected != 1 {
		return 0, 0, "", authority.Result{Outcome: authority.OutcomeStale, Code: "JOURNAL_HWM_STALE"}
	}
	return row.PendingCount, request.Admission.PendingTransition.Next, journalID, authority.Result{}
}

func advanceAdmissionAuthorityHead(tx *gorm.DB, head authorityHeadRow, owner *entity.ExecutionContext, job *DecisionJobModel, caseModel *CaseModel, generation uint64) (uint64, authority.Result) {
	if head.LastSequence == ^uint64(0) {
		return 0, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_SEQUENCE_EXHAUSTED"}
	}
	next := head.LastSequence + 1
	updated := tx.Exec(`UPDATE magi_authority_head h
		JOIN decision_job j ON j.id = ? AND j.case_id = ?
		JOIN decision_case c ON c.id = ?
		SET h.last_sequence = ?, h.last_admission_sequence = ?, h.updated_at = CURRENT_TIMESTAMP(6)
		WHERE h.trust_domain = ? AND h.last_sequence = ?
		  AND j.status = ? AND j.worker_id = ? AND j.execution_generation = ?
		  AND c.execution_generation = ?
		  AND j.lease_until IS NOT NULL AND j.lease_until > CURRENT_TIMESTAMP(6)`,
		owner.JobID, owner.CaseID, owner.CaseID, next, next,
		head.TrustDomain, head.LastSequence, string(entity.DecisionJobRunning), owner.WorkerID,
		owner.ExecutionGeneration, generation)
	if updated.Error != nil {
		return 0, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: updated.Error.Error()}
	}
	if updated.RowsAffected != 1 {
		code := "AUTHORITY_SEQUENCE_STALE"
		if now, err := databaseNow(tx); err == nil {
			if ownerCode := classifyOwnerValidationFailure(job, caseModel, owner, now); ownerCode != "OWNER_LEASE_LOST" {
				code = ownerCode
			}
		}
		return 0, authority.Result{Outcome: authority.OutcomeStale, Code: code}
	}
	return next, authority.Result{}
}

func insertAuthorityAdmissionV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, owner *entity.ExecutionContext, leaseUntil time.Time, commit authoritycommit.AdmissionCommit, rawRequest, scopes []byte) error {
	key := request.Admission.Key
	return tx.Exec(`INSERT INTO magi_authority_admission_v2
		(trust_domain, commit_id, unique_key, case_id, case_round, context_manifest_id, persona_id,
		 delivery_id, provider_request_id, journal_id, request_digest, exact_target_id, commit_token,
		 authority_sequence, journal_high_watermark, execution_generation, proof_id, admission_digest,
		 admission_object_ref, raw_request, owner_job_id, owner_worker_id, owner_lease_until, state,
		 exposure_permitted, committed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ADMITTED', 0, ?)`,
		request.Verification.TrustDomain, commit.CommitID, commit.UniqueKey, key.CaseID, key.Round,
		key.ContextManifestID, key.PersonaID, key.DeliveryID, key.ProviderRequestID, commit.JournalID,
		commit.RequestDigest, commit.ExactTargetID, commit.CommitToken, commit.AuthoritySequence,
		commit.JournalHighWatermark, request.Admission.ExecutionGeneration, request.Verification.ProofID,
		commit.AdmissionDigest, request.Admission.AdmissionInput.ObjectRef, rawRequest,
		owner.JobID, owner.WorkerID, leaseUntil, mustAuthorityTime(commit.CommittedAt)).Error
}

func insertAuthorityJournalEventV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, commit authoritycommit.AdmissionCommit) error {
	return tx.Exec(`INSERT INTO magi_authority_journal_event_v2
		(trust_domain, journal_id, journal_high_watermark, commit_id, event_id, event_digest, object_ref)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, commit.JournalID, commit.JournalLocalSequence, commit.CommitID,
		request.Admission.JournalEventInput.ID, request.Admission.JournalEventInput.Digest,
		request.Admission.JournalEventInput.ObjectRef).Error
}

func insertAuthorityDependencyV2(tx *gorm.DB, trustDomain string, commit authoritycommit.AdmissionCommit, dependency authoritycommit.PreparedObject, ordinal int) error {
	return tx.Exec(`INSERT INTO magi_authority_dependency_v2
		(trust_domain, commit_id, dependency_id, dependency_digest, object_ref, ordinal)
		VALUES (?, ?, ?, ?, ?, ?)`,
		trustDomain, commit.CommitID, dependency.ID, dependency.Digest, dependency.ObjectRef, ordinal).Error
}

func insertAuthorityReservationV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, commit authoritycommit.AdmissionCommit) error {
	return tx.Exec(`INSERT INTO magi_authority_provider_reservation_v2
		(trust_domain, commit_id, provider_request_id, session_scope_id, state)
		VALUES (?, ?, ?, ?, 'RESERVED')`,
		request.Verification.TrustDomain, commit.CommitID, request.Admission.ProviderReservation.RequestID,
		request.Admission.ProviderReservation.SessionScopeID).Error
}

func insertAuthorityFenceV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, commit authoritycommit.AdmissionCommit, scopes []byte) error {
	fence := request.Admission.Fence
	return tx.Exec(`INSERT INTO magi_authority_exposure_fence_v2
		(trust_domain, commit_id, fence_id, mode, state, exposure_permitted, lease_id, lease_until,
		 authority_watermark, execution_generation, scoped_epochs, provider_capability_id,
		 provider_capability_digest, provider_capability_ref)
		VALUES (?, ?, ?, ?, 'DECLARED_BLOCKED', 0, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, commit.CommitID, fence.LeaseID, fence.Mode, fence.LeaseID,
		mustAuthorityTime(fence.LeaseUntil), commit.AuthoritySequence, fence.ExecutionGeneration, scopes,
		fence.ProviderCapabilityRef.ID, fence.ProviderCapabilityRef.Digest, fence.ProviderCapabilityRef.ObjectRef).Error
}

func insertAuthorityPendingTransitionV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, commit authoritycommit.AdmissionCommit) error {
	return tx.Exec(`INSERT INTO magi_authority_pending_transition_v2
		(trust_domain, commit_id, journal_id, expected_pending, next_pending)
		VALUES (?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, commit.CommitID, commit.JournalID,
		request.Admission.PendingTransition.Expected, request.Admission.PendingTransition.Next).Error
}

func insertAuthorityUniqueBindingV2(tx *gorm.DB, request authoritycommit.AdmissionRequest, commit authoritycommit.AdmissionCommit) error {
	return tx.Exec(`INSERT INTO magi_authority_unique_binding_v2
		(trust_domain, unique_key, commit_id, request_digest, exact_target_id, commit_token,
		 authority_sequence, journal_high_watermark, execution_generation)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, commit.UniqueKey, commit.CommitID, commit.RequestDigest,
		commit.ExactTargetID, commit.CommitToken, commit.AuthoritySequence, commit.JournalHighWatermark,
		request.Admission.ExecutionGeneration).Error
}

func insertAuthorityTokenConsumedV2(tx *gorm.DB, trustDomain string, commit authoritycommit.AdmissionCommit) error {
	return tx.Exec(`INSERT INTO magi_authority_token_consumed_v2
		(trust_domain, commit_token, commit_id, request_digest)
		VALUES (?, ?, ?, ?)`,
		trustDomain, commit.CommitToken, commit.CommitID, commit.RequestDigest).Error
}

func insertAuthorityLineageV2(tx *gorm.DB, trustDomain string, commit authoritycommit.AdmissionCommit, raw []byte) error {
	return tx.Exec(`INSERT INTO magi_authority_lineage_v2
		(trust_domain, commit_id, request_digest, lineage_digest, authority_sequence,
		 journal_high_watermark, raw_lineage)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		trustDomain, commit.CommitID, commit.RequestDigest, commit.LineageDigest,
		commit.AuthoritySequence, commit.JournalHighWatermark, raw).Error
}

func insertAuthorityOutboxV2(tx *gorm.DB, trustDomain string, commit authoritycommit.AdmissionCommit, payload []byte) error {
	return tx.Exec(`INSERT INTO magi_authority_outbox_v2
		(trust_domain, commit_id, event_kind, payload, state)
		VALUES (?, ?, 'ADMISSION_COMMITTED', ?, 'BLOCKED')`,
		trustDomain, commit.CommitID, payload).Error
}
