package magi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
	"github.com/jamespud/magi/backend/domain/canonical"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
)

// ExposureGateAttestationVerifier verifies independently persisted provider
// capability and witness evidence. It performs no remote call and must not
// treat a boolean in the request as trusted evidence.
type ExposureGateAttestationVerifier interface {
	VerifyGateAttestation(request authoritycommit.ExposureGateRequest, capability authoritycommit.ProviderCapability, fence AuthorityExposureGateFenceEvidence, now time.Time) authority.Result
}

// SetExposureGateAttestationVerifierForTest is available only to this package's
// tests through authority_commit_export_test.go. Production wiring remains off.
func (r *AuthorityCommitRepository) SetExposureGateAttestationVerifierForTest(verifier ExposureGateAttestationVerifier) {
	r.witnessVerifier = verifier
	r.gateAttestationVerifier = verifier
}

func exposureGateFailure(outcome authority.Outcome, code, detail string) authoritycommit.ExposureGateResult {
	return authoritycommit.ExposureGateResult{Outcome: outcome, Code: code, Detail: detail, ExposurePermitted: false}
}

func exposureGateResultPtr(result authoritycommit.ExposureGateResult) *authoritycommit.ExposureGateResult {
	return &result
}

func exposureGateFromAuthorityResult(result authority.Result) authoritycommit.ExposureGateResult {
	return exposureGateFailure(result.Outcome, result.Code, result.Detail)
}

func exposureGateCommitID(requestDigest string) string {
	sum := sha256.Sum256([]byte("magi:authority-journal-exposure-gate-v2:commit:" + requestDigest))
	return "agc_" + hex.EncodeToString(sum[:])
}

func lockExposureGateAttempt(tx *gorm.DB, trustDomain, attemptID string) (*authorityExposureGateAttemptV2Row, authority.Result) {
	var row authorityExposureGateAttemptV2Row
	err := tx.Raw(`SELECT trust_domain, attempt_id, request_digest, raw_request, commit_token,
			outcome, code, blocked_reason, gate_id, admission_commit_id, authority_sequence,
			exposure_permitted, committed_at
		FROM magi_authority_exposure_gate_attempt_v2
		WHERE trust_domain = ? AND attempt_id = ? FOR UPDATE`, trustDomain, attemptID).Row().Scan(
		&row.TrustDomain, &row.AttemptID, &row.RequestDigest, &row.RawRequest, &row.CommitToken,
		&row.Outcome, &row.Code, &row.BlockedReason, &row.GateID, &row.AdmissionCommitID,
		&row.AuthoritySequence, &row.ExposurePermitted, &row.CommittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func lockExposureGateAttemptByToken(tx *gorm.DB, trustDomain, commitToken string) (*authorityExposureGateAttemptV2Row, authority.Result) {
	var row authorityExposureGateAttemptV2Row
	err := tx.Raw(`SELECT trust_domain, attempt_id, request_digest, raw_request, commit_token,
			outcome, code, blocked_reason, gate_id, admission_commit_id, authority_sequence,
			exposure_permitted, committed_at
		FROM magi_authority_exposure_gate_attempt_v2
		WHERE trust_domain = ? AND commit_token = ? FOR UPDATE`, trustDomain, commitToken).Row().Scan(
		&row.TrustDomain, &row.AttemptID, &row.RequestDigest, &row.RawRequest, &row.CommitToken,
		&row.Outcome, &row.Code, &row.BlockedReason, &row.GateID, &row.AdmissionCommitID,
		&row.AuthoritySequence, &row.ExposurePermitted, &row.CommittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func lockExposureGateToken(tx *gorm.DB, trustDomain, commitToken string) (*exposureGateTokenRow, authority.Result) {
	var row exposureGateTokenRow
	err := tx.Raw(`SELECT trust_domain, commit_token, gate_commit_id, request_digest, consumed_at
		FROM magi_authority_exposure_gate_token_v2
		WHERE trust_domain = ? AND commit_token = ? FOR UPDATE`, trustDomain, commitToken).
		Row().Scan(&row.TrustDomain, &row.CommitToken, &row.GateCommitID, &row.RequestDigest, &row.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, authority.Result{}
	}
	if err != nil {
		return nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	return &row, authority.Result{}
}

func replayExposureGateAttempt(tx *gorm.DB, row authorityExposureGateAttemptV2Row, requestRaw []byte, requestDigest, commitToken string) (authoritycommit.ExposureGateResult, authority.Result) {
	if !bytes.Equal(row.RawRequest, requestRaw) || row.RequestDigest != requestDigest || row.CommitToken != commitToken {
		return authoritycommit.ExposureGateResult{}, authority.Result{Outcome: authority.OutcomeConflict, Code: "EXPOSURE_GATE_ATTEMPT_CONFLICT"}
	}
	if row.Outcome == "BLOCKED" {
		var attempt authoritycommit.ExposureGateAttempt
		attempt = authoritycommit.ExposureGateAttempt{
			AttemptID: row.AttemptID, RequestDigest: row.RequestDigest, Outcome: row.Outcome,
			Code: row.Code, GateID: row.GateID, AdmissionCommitID: row.AdmissionCommitID,
			AuthoritySequence: row.AuthoritySequence, CommittedAt: authorityFormatTime(row.CommittedAt),
			BlockedReason: row.BlockedReason.String, ExposurePermitted: false,
		}
		return authoritycommit.ExposureGateResult{
			Outcome: authority.OutcomeDenied, Code: row.Code, Disposition: "REPLAY", Attempt: &attempt,
			BlockedCapabilities: []string{row.BlockedReason.String}, ExposurePermitted: false,
		}, authority.Result{}
	}
	var binding struct {
		GateCommitID      string
		AuthoritySequence uint64
	}
	if err := tx.Raw(`SELECT gate_commit_id, authority_sequence FROM magi_authority_exposure_gate_commit_v2
		WHERE trust_domain=? AND gate_id=?`, row.TrustDomain, row.GateID).Row().Scan(&binding.GateCommitID, &binding.AuthoritySequence); err != nil {
		return authoritycommit.ExposureGateResult{}, authority.Result{Outcome: authority.OutcomeUnknown, Code: "EXPOSURE_GATE_LINEAGE_MISSING", Detail: err.Error()}
	}
	attempt := authoritycommit.ExposureGateAttempt{
		AttemptID: row.AttemptID, RequestDigest: row.RequestDigest, Outcome: row.Outcome,
		Code: row.Code, GateID: row.GateID, AdmissionCommitID: row.AdmissionCommitID,
		AuthoritySequence: row.AuthoritySequence, CommittedAt: authorityFormatTime(row.CommittedAt),
		BlockedReason: row.BlockedReason.String, ExposurePermitted: false,
	}
	return authoritycommit.ExposureGateResult{
		Outcome: authority.OutcomeValid, Code: "EXPOSURE_GATE_REPLAY", Disposition: "REPLAY", Attempt: &attempt,
		BlockedCapabilities: []string{"PROVIDER_FENCE_CAPABILITY_UNVERIFIED", "INDEPENDENT_WITNESS_UNAVAILABLE"},
		ExposurePermitted:   false,
	}, authority.Result{}
}

func lockExposureGateAdmission(tx *gorm.DB, request authoritycommit.ExposureGateRequest) (*authorityAdmissionV2Row, *authorityExposureGateFenceV2Row, authority.Result) {
	var admission authorityAdmissionV2Row
	var committedAt time.Time
	err := tx.Raw(`SELECT trust_domain, commit_id, unique_key, case_id, case_round, context_manifest_id,
			persona_id, delivery_id, provider_request_id, journal_id, request_digest, exact_target_id,
			commit_token, authority_sequence, journal_high_watermark, execution_generation, proof_id,
			admission_digest, admission_object_ref, raw_request, owner_job_id, owner_worker_id,
			owner_lease_until, state, exposure_permitted, committed_at
		FROM magi_authority_admission_v2
		WHERE trust_domain=? AND commit_id=? FOR UPDATE`,
		request.Verification.TrustDomain, request.Gate.AdmissionCommitID).Row().Scan(
		&admission.TrustDomain, &admission.CommitID, &admission.UniqueKey, &admission.CaseID,
		&admission.CaseRound, &admission.ContextManifestID, &admission.PersonaID, &admission.DeliveryID,
		&admission.ProviderRequestID, &admission.JournalID, &admission.RequestDigest, &admission.ExactTargetID,
		&admission.CommitToken, &admission.AuthoritySequence, &admission.JournalHighWatermark,
		&admission.ExecutionGeneration, &admission.ProofID, &admission.AdmissionDigest,
		&admission.AdmissionObjectRef, &admission.RawRequest, &admission.OwnerJobID, &admission.OwnerWorkerID,
		&admission.OwnerLeaseUntil, &admission.State, &admission.ExposurePermitted, &committedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "ADMISSION_NOT_FOUND"}
	}
	if err != nil {
		return nil, nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	admission.CommittedAt = committedAt
	if admission.State != "ADMITTED" || admission.ExposurePermitted {
		return nil, nil, authority.Result{Outcome: authority.OutcomeConflict, Code: "ADMISSION_STATE_CONFLICT"}
	}
	var fence authorityExposureGateFenceV2Row
	var leaseUntil time.Time
	err = tx.Raw(`SELECT lease_until, provider_capability_id, provider_capability_digest,
			provider_capability_ref, execution_generation, state
		FROM magi_authority_exposure_fence_v2
		WHERE trust_domain=? AND commit_id=? AND fence_id=? FOR UPDATE`,
		request.Verification.TrustDomain, request.Gate.AdmissionCommitID, request.Gate.FenceID).Row().Scan(
		&leaseUntil, &fence.ProviderCapabilityID, &fence.ProviderCapabilityDigest,
		&fence.ProviderCapabilityRef, &fence.ExecutionGeneration, &fence.State)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "EXPOSURE_FENCE_NOT_FOUND"}
	}
	if err != nil {
		return nil, nil, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: err.Error()}
	}
	fence.LeaseUntilString = authorityFormatTime(leaseUntil)
	if fence.State != "DECLARED_BLOCKED" {
		return nil, nil, authority.Result{Outcome: authority.OutcomeConflict, Code: "EXPOSURE_FENCE_STATE_CONFLICT"}
	}
	return &admission, &fence, authority.Result{}
}

func advanceExposureGateAuthorityHead(tx *gorm.DB, head authorityHeadRow, owner *entity.ExecutionContext, job *DecisionJobModel, caseModel *CaseModel) (uint64, authority.Result) {
	if head.LastSequence == ^uint64(0) {
		return 0, authority.Result{Outcome: authority.OutcomeConflict, Code: "AUTHORITY_SEQUENCE_EXHAUSTED"}
	}
	next := head.LastSequence + 1
	updated := tx.Exec(`UPDATE magi_authority_head h
		JOIN decision_job j ON j.id=? AND j.case_id=?
		JOIN decision_case c ON c.id=?
		SET h.last_sequence=?, h.last_gate_sequence=?, h.updated_at=CURRENT_TIMESTAMP(6)
		WHERE h.trust_domain=? AND h.last_sequence=?
		  AND j.status=? AND j.worker_id=? AND j.execution_generation=?
		  AND c.execution_generation=?
		  AND j.lease_until IS NOT NULL AND j.lease_until > CURRENT_TIMESTAMP(6)`,
		owner.JobID, owner.CaseID, owner.CaseID, next, next, head.TrustDomain, head.LastSequence,
		string(entity.DecisionJobRunning), owner.WorkerID, owner.ExecutionGeneration,
		requestExecutionGeneration(job, caseModel))
	if updated.Error != nil {
		return 0, authority.Result{Outcome: authority.OutcomeUnknown, Code: "AUTHORITY_STORE_ERROR", Detail: updated.Error.Error()}
	}
	if updated.RowsAffected != 1 {
		return 0, authority.Result{Outcome: authority.OutcomeStale, Code: "AUTHORITY_SEQUENCE_STALE"}
	}
	return next, authority.Result{}
}

func requestExecutionGeneration(job *DecisionJobModel, caseModel *CaseModel) uint64 {
	if job == nil || caseModel == nil {
		return 0
	}
	return uint64(job.ExecutionGeneration)
}

func insertExposureGateAttemptV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, requestDigest, gateCommitID string, sequence uint64, now time.Time) error {
	raw, err := canonical.CanonicalizeStruct(request)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_attempt_v2
		(trust_domain, attempt_id, request_digest, raw_request, commit_token, outcome, code,
		 blocked_reason, gate_id, admission_commit_id, authority_sequence, exposure_permitted, committed_at)
		VALUES (?, ?, ?, ?, ?, 'EXPOSURE_GATED', 'EXPOSURE_GATE_COMMITTED', NULL, ?, ?, ?, 0, ?)`,
		request.Verification.TrustDomain, request.AttemptID, requestDigest, raw.Bytes(), request.CommitToken,
		request.Gate.GateID, request.Gate.AdmissionCommitID, sequence, now).Error
}

func insertExposureGateBindingV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, requestDigest, gateCommitID string, sequence uint64) error {
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_commit_v2
		(trust_domain, gate_id, gate_commit_id, attempt_id, admission_commit_id, request_digest,
		 authority_sequence, exposure_permitted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		request.Verification.TrustDomain, request.Gate.GateID, gateCommitID, request.AttemptID,
		request.Gate.AdmissionCommitID, requestDigest, sequence).Error
}

func insertExposureGateTokenV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, requestDigest, gateCommitID string) error {
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_token_v2
		(trust_domain, commit_token, gate_commit_id, request_digest)
		VALUES (?, ?, ?, ?)`,
		request.Verification.TrustDomain, request.CommitToken, gateCommitID, requestDigest).Error
}

func insertExposureGateLineageV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, requestDigest, gateCommitID string, sequence uint64, raw []byte) error {
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_lineage_v2
		(trust_domain, gate_commit_id, request_digest, authority_sequence, raw_lineage)
		VALUES (?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, gateCommitID, requestDigest, sequence, raw).Error
}

func insertExposureGateAttestationV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, gateCommitID string) error {
	capability := request.Gate.ProviderCapability
	validUntil := mustAuthorityTime(request.Witness.ValidUntil)
	capabilityValid := mustAuthorityTime(capability.MaxLeaseUntil)
	if capabilityValid.Before(validUntil) {
		validUntil = capabilityValid
	}
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_attestation_v2
		(trust_domain, gate_commit_id, capability_id, capability_digest,
		 verification_id, verification_digest, witness_id, witness_evidence_digest, valid_until)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		request.Verification.TrustDomain, gateCommitID, capability.CapabilityID, capability.CapabilityDigest,
		capability.VerificationRef.ID, capability.VerificationRef.Digest,
		request.Witness.WitnessID, request.Witness.EvidenceDigest, validUntil).Error
}

func validateExposureGateAdmissionBinding(admission *authorityAdmissionV2Row, fence *authorityExposureGateFenceV2Row, request authoritycommit.ExposureGateRequest, owner *entity.ExecutionContext) authority.Result {
	if admission == nil || fence == nil || owner == nil {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "EXPOSURE_GATE_ADMISSION_UNBOUND"}
	}
	key := request.Gate
	if key.AdmissionCommitID != admission.CommitID ||
		admission.CaseID != request.Verification.CaseID || admission.CaseRound != request.Verification.Round ||
		admission.ExecutionGeneration != request.Verification.ExecutionGeneration ||
		fence.ExecutionGeneration != request.Verification.ExecutionGeneration ||
		admission.OwnerJobID != owner.JobID || admission.OwnerWorkerID != owner.WorkerID {
		return authority.Result{Outcome: authority.OutcomeStale, Code: "EXPOSURE_GATE_ADMISSION_OWNER_STALE"}
	}
	if admission.JournalID == "" || admission.ProviderRequestID == "" || admission.ContextManifestID == "" ||
		admission.PersonaID == "" || admission.DeliveryID == "" {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "EXPOSURE_GATE_ADMISSION_INCOMPLETE"}
	}
	var admitted authoritycommit.AdmissionRequest
	if err := json.Unmarshal(admission.RawRequest, &admitted); err != nil {
		return authority.Result{Outcome: authority.OutcomeDenied, Code: "EXPOSURE_GATE_ADMISSION_CORRUPT", Detail: err.Error()}
	}
	requestDigest, err := authoritycommit.RequestDigest(admitted)
	if err != nil || requestDigest != key.AdmissionRequestDigest || requestDigest != admission.RequestDigest {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "EXPOSURE_GATE_ADMISSION_BINDING_MISMATCH"}
	}
	if request.Witness != (authoritycommit.WitnessEvidence{}) && request.Witness.RequestDigest != requestDigest {
		return authority.Result{Outcome: authority.OutcomeConflict, Code: "EXPOSURE_GATE_WITNESS_BINDING_MISMATCH"}
	}
	if !slices.Equal(request.Verification.Scopes, admitted.Verification.Scopes) ||
		request.Verification.Subject != admitted.Verification.Subject ||
		request.Verification.Action != admitted.Verification.Action ||
		request.Verification.Tenant != admitted.Verification.Tenant ||
		request.Verification.CaseID != admitted.Admission.Key.CaseID ||
		request.Verification.Round != admitted.Admission.Key.Round {
		return authority.Result{Outcome: authority.OutcomeStale, Code: "EXPOSURE_GATE_C2_BINDING_MISMATCH"}
	}
	return authority.Result{Outcome: authority.OutcomeValid, Code: "EXPOSURE_GATE_ADMISSION_BOUND"}
}

// commitBlockedExposureGate persists a terminal BLOCKED attempt. It uses the
// same Head lock as a successful Gate but never changes the sequence or fence.
func (r *AuthorityCommitRepository) commitBlockedExposureGate(
	ctx context.Context,
	owner *entity.ExecutionContext,
	request authoritycommit.ExposureGateRequest,
	requestDigest string,
) authoritycommit.ExposureGateResult {
	raw, err := canonical.CanonicalizeStruct(request)
	if err != nil {
		return exposureGateFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	var result authoritycommit.ExposureGateResult
	var abort *authoritycommit.ExposureGateResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, typed := lockAuthorityHead(tx, request.Verification.TrustDomain); typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		existing, typed := lockExposureGateAttempt(tx, request.Verification.TrustDomain, request.AttemptID)
		if typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if existing != nil {
			replayed, typed := replayExposureGateAttempt(tx, *existing, raw.Bytes(), requestDigest, request.CommitToken)
			if typed.Outcome != "" {
				abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
				return errAuthorityCommitAbort
			}
			result = replayed
			return nil
		}
		tokenAttempt, typed := lockExposureGateAttemptByToken(tx, request.Verification.TrustDomain, request.CommitToken)
		if typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if tokenAttempt != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "COMMIT_TOKEN_REUSED", ""))
			return errAuthorityCommitAbort
		}
		token, typed := lockExposureGateToken(tx, request.Verification.TrustDomain, request.CommitToken)
		if typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if token != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "COMMIT_TOKEN_REUSED", ""))
			return errAuthorityCommitAbort
		}
		if typed := r.authority.verifyProofInTx(tx, request.Verification); typed.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if _, _, err := lockActiveExecution(tx, owner); err != nil {
			code := authorityOwnerFailureCode(tx, owner, err)
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeStale, code, err.Error()))
			return errAuthorityCommitAbort
		}
		admission, fence, typed := lockExposureGateAdmission(tx, request)
		if typed.Outcome != "" && typed.Code != "EXPOSURE_FENCE_STATE_CONFLICT" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if typed.Outcome == "" {
			if bound := validateExposureGateAdmissionBinding(admission, fence, request, owner); bound.Outcome != authority.OutcomeValid {
				abort = exposureGateResultPtr(exposureGateFromAuthorityResult(bound))
				return errAuthorityCommitAbort
			}
		}
		now, err := databaseNow(tx)
		if err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		code, reason := blockedExposureGateReason(request, now)
		if code == "" {
			code, reason = "INDEPENDENT_WITNESS_UNAVAILABLE", "no independent verifier is configured"
		}
		attempt := authoritycommit.ExposureGateAttempt{
			AttemptID: request.AttemptID, RequestDigest: requestDigest, Outcome: "BLOCKED",
			Code: code, GateID: request.Gate.GateID, AdmissionCommitID: request.Gate.AdmissionCommitID,
			CommittedAt: authorityFormatTime(now), BlockedReason: reason, ExposurePermitted: false,
		}
		if err := insertBlockedExposureGateAttemptV2(tx, request, requestDigest, code, reason, now); err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		result = authoritycommit.ExposureGateResult{
			Outcome: authority.OutcomeDenied, Code: code, Disposition: "BLOCKED", Attempt: &attempt,
			BlockedCapabilities: []string{reason}, ExposurePermitted: false,
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if abort != nil {
		return *abort
	}
	if err != nil {
		return exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error())
	}
	return result
}

func exposureWitnessDeclared(witness authoritycommit.WitnessEvidence) bool {
	return witness != (authoritycommit.WitnessEvidence{})
}

func blockedExposureGateReason(request authoritycommit.ExposureGateRequest, now time.Time) (string, string) {
	if result := authoritycommit.ValidateProviderCapability(request.Gate.ProviderCapability); result.Outcome != authority.OutcomeValid {
		return result.Code, result.Code
	}
	witness := request.Witness
	if witness == (authoritycommit.WitnessEvidence{}) {
		return "INDEPENDENT_WITNESS_UNAVAILABLE", "Witness evidence is absent."
	}
	if witness.AdmissionCommitID != request.Gate.AdmissionCommitID {
		return "WITNESS_BINDING_MISMATCH", "Witness evidence references a different admission."
	}
	if !mustAuthorityTime(witness.ValidUntil).After(now) {
		return "WITNESS_EVIDENCE_EXPIRED", "Witness evidence is no longer current."
	}
	return "", ""
}

func insertBlockedExposureGateAttemptV2(tx *gorm.DB, request authoritycommit.ExposureGateRequest, requestDigest, code, reason string, now time.Time) error {
	raw, err := canonical.CanonicalizeStruct(request)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO magi_authority_exposure_gate_attempt_v2
		(trust_domain, attempt_id, request_digest, raw_request, commit_token, outcome, code,
		 blocked_reason, gate_id, admission_commit_id, authority_sequence, exposure_permitted, committed_at)
		VALUES (?, ?, ?, ?, ?, 'BLOCKED', ?, ?, ?, ?, 0, 0, ?)`,
		request.Verification.TrustDomain, request.AttemptID, requestDigest, raw.Bytes(), request.CommitToken,
		code, reason, request.Gate.GateID, request.Gate.AdmissionCommitID, now).Error
}
