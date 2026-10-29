package magi

import (
	"database/sql"
	"time"
)

// S34 Gate rows mirror the common authority order. A GateAttempt terminal row
// is immutable; a GateCommit is the only row that consumes an authority
// sequence. Neither row is a consumable Provider permit.
type authorityExposureGateAttemptV2Row struct {
	TrustDomain       string         `gorm:"column:trust_domain;primaryKey"`
	AttemptID         string         `gorm:"column:attempt_id;primaryKey"`
	RequestDigest     string         `gorm:"column:request_digest"`
	RawRequest        []byte         `gorm:"column:raw_request"`
	CommitToken       string         `gorm:"column:commit_token"`
	Outcome           string         `gorm:"column:outcome"`
	Code              string         `gorm:"column:code"`
	BlockedReason     sql.NullString `gorm:"column:blocked_reason"`
	GateID            string         `gorm:"column:gate_id"`
	AdmissionCommitID string         `gorm:"column:admission_commit_id"`
	AuthoritySequence uint64         `gorm:"column:authority_sequence"`
	ExposurePermitted bool           `gorm:"column:exposure_permitted"`
	CommittedAt       time.Time      `gorm:"column:committed_at"`
}

func (authorityExposureGateAttemptV2Row) TableName() string {
	return "magi_authority_exposure_gate_attempt_v2"
}

type authorityExposureGateCommitV2Row struct {
	TrustDomain       string    `gorm:"column:trust_domain;primaryKey"`
	GateID            string    `gorm:"column:gate_id;primaryKey"`
	GateCommitID      string    `gorm:"column:gate_commit_id"`
	AttemptID         string    `gorm:"column:attempt_id"`
	AdmissionCommitID string    `gorm:"column:admission_commit_id"`
	RequestDigest     string    `gorm:"column:request_digest"`
	AuthoritySequence uint64    `gorm:"column:authority_sequence"`
	ExposurePermitted bool      `gorm:"column:exposure_permitted"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (authorityExposureGateCommitV2Row) TableName() string {
	return "magi_authority_exposure_gate_commit_v2"
}

type authorityExposureGateFenceV2Row struct {
	LeaseUntilString         string
	ProviderCapabilityID     string
	ProviderCapabilityDigest string
	ProviderCapabilityRef    string
	ExecutionGeneration      uint64
	State                    string
}

type exposureGateTokenRow struct {
	TrustDomain   string
	CommitToken   string
	GateCommitID  string
	RequestDigest string
	ConsumedAt    time.Time
}

// AuthorityExposureGateFenceEvidence is the immutable fence snapshot supplied
// to an attestation verifier. It contains no remote response.
type AuthorityExposureGateFenceEvidence struct {
	LeaseUntil               time.Time
	ProviderCapabilityID     string
	ProviderCapabilityDigest string
	ProviderCapabilityRef    string
	ExecutionGeneration      uint64
}
