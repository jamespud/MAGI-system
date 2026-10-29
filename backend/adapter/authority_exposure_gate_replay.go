package magi

import (
	"bytes"
	"context"
	"database/sql"

	"github.com/jamespud/magi/backend/domain/authority"
	"github.com/jamespud/magi/backend/domain/authoritycommit"
	"github.com/jamespud/magi/backend/domain/canonical"
	"gorm.io/gorm"
)

// ReplayJournalExposureGateV2 reads historical Gate lineage only after a
// current JOURNAL_AUDIT_READ proof passes C2 and epoch checks. It never
// reassigns a sequence, re-evaluates a blocked attempt as success or issues a
// permit.
func (r *AuthorityCommitRepository) ReplayJournalExposureGateV2(
	ctx context.Context,
	request authoritycommit.ExposureGateReplayRequest,
) authoritycommit.ExposureGateResult {
	if r == nil || r.db == nil || r.authority == nil {
		return exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_UNAVAILABLE", "")
	}
	if capability := r.authority.capabilityResult(); capability != nil {
		return exposureGateFromAuthorityResult(*capability)
	}
	if result := authoritycommit.ValidateExposureGateReplay(request); result.Outcome != authority.OutcomeValid {
		return exposureGateFailure(result.Outcome, result.Code, result.Detail)
	}
	raw, err := canonical.CanonicalizeStruct(request.Original)
	if err != nil {
		return exposureGateFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	requestDigest, err := authorityCommitDigest("magi:authority-journal-exposure-gate-v2:", request.Original)
	if err != nil {
		return exposureGateFailure(authority.OutcomeDenied, "REQUEST_DIGEST_INVALID", err.Error())
	}
	var result authoritycommit.ExposureGateResult
	var abort *authoritycommit.ExposureGateResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, typed := lockAuthorityHead(tx, request.AuditVerification.TrustDomain); typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if typed := r.authority.verifyProofInTx(tx, request.AuditVerification); typed.Outcome != authority.OutcomeValid {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		row, typed := lockExposureGateAttempt(tx, request.AuditVerification.TrustDomain, request.Original.AttemptID)
		if typed.Outcome != "" {
			abort = exposureGateResultPtr(exposureGateFromAuthorityResult(typed))
			return errAuthorityCommitAbort
		}
		if row == nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "EXPOSURE_GATE_NOT_FOUND", ""))
			return errAuthorityCommitAbort
		}
		if !bytes.Equal(row.RawRequest, raw.Bytes()) || row.RequestDigest != requestDigest ||
			row.CommitToken != request.Original.CommitToken {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "EXPOSURE_GATE_ATTEMPT_CONFLICT", ""))
			return errAuthorityCommitAbort
		}
		attempt := authoritycommit.ExposureGateAttempt{
			AttemptID: row.AttemptID, RequestDigest: row.RequestDigest, Outcome: row.Outcome,
			Code: row.Code, GateID: row.GateID, AdmissionCommitID: row.AdmissionCommitID,
			AuthoritySequence: row.AuthoritySequence, CommittedAt: authorityFormatTime(row.CommittedAt),
			BlockedReason: row.BlockedReason.String, ExposurePermitted: false,
		}
		if row.Outcome == "BLOCKED" {
			result = authoritycommit.ExposureGateResult{
				Outcome: authority.OutcomeDenied, Code: row.Code, Disposition: "REPLAY", Attempt: &attempt,
				BlockedCapabilities: []string{row.BlockedReason.String}, ExposurePermitted: false,
			}
			return nil
		}
		var commitCount int64
		if err := tx.Raw(`SELECT COUNT(*) FROM magi_authority_exposure_gate_commit_v2
			WHERE trust_domain=? AND gate_id=? AND attempt_id=? AND request_digest=?`,
			row.TrustDomain, row.GateID, row.AttemptID, row.RequestDigest).Scan(&commitCount).Error; err != nil {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeUnknown, "AUTHORITY_STORE_ERROR", err.Error()))
			return errAuthorityCommitAbort
		}
		if commitCount != 1 {
			abort = exposureGateResultPtr(exposureGateFailure(authority.OutcomeConflict, "EXPOSURE_GATE_LINEAGE_CONFLICT", ""))
			return errAuthorityCommitAbort
		}
		result = authoritycommit.ExposureGateResult{
			Outcome: authority.OutcomeValid, Code: "EXPOSURE_GATE_REPLAY", Disposition: "REPLAY", Attempt: &attempt,
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
	return result
}
