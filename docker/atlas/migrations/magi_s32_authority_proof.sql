-- MAGI S32: authenticated C2 authority evidence and scoped revocation state.
--
-- FRESH S32 INSTALL ONLY: this file's table shape changed during review.
-- CREATE TABLE IF NOT EXISTS is not an in-place upgrade. Any database that
-- applied an earlier unmerged S32 draft requires a separate reviewed migration
-- before this file can be applied. Do not treat this as a supported upgrade.
--
-- Additive only. This migration installs no trust root, recovery key, issuer,
-- governance principal, owner policy, or revocation. It deliberately defines no
-- authority sequence: cross-C2/C3 total ordering remains blocked on #47.
-- C2 serializes mutations by locking the pinned active-root row and uses exact
-- per-scope epoch/row-version CAS.

CREATE TABLE IF NOT EXISTS magi_authority_root (
    trust_domain VARCHAR(191) NOT NULL,
    root_incarnation VARCHAR(191) NOT NULL,
    root_fingerprint CHAR(64) NOT NULL,
    bootstrap_version VARCHAR(64) NOT NULL,
    identity_source_id VARCHAR(191) NOT NULL,
    identity_source_version VARCHAR(191) NOT NULL,
    governance_subject_id VARCHAR(191) NOT NULL,
    status VARCHAR(16) NOT NULL,
    active_slot TINYINT UNSIGNED NULL,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    installed_at DATETIME(3) NOT NULL,
    retired_at DATETIME(3) NULL,
    raw_bootstrap LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, root_incarnation),
    UNIQUE KEY uk_authority_root_fingerprint (trust_domain, root_fingerprint),
    UNIQUE KEY uk_authority_one_active_root (trust_domain, active_slot),
    KEY idx_authority_root_status (trust_domain, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_credential (
    trust_domain VARCHAR(191) NOT NULL,
    root_incarnation VARCHAR(191) NOT NULL,
    credential_id VARCHAR(191) NOT NULL,
    credential_version BIGINT UNSIGNED NOT NULL,
    subject_id VARCHAR(191) NOT NULL,
    identity_source_id VARCHAR(191) NOT NULL,
    identity_source_version VARCHAR(191) NOT NULL,
    algorithm VARCHAR(32) NOT NULL,
    public_key BLOB NOT NULL,
    not_before DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    status VARCHAR(16) NOT NULL,
    credential_scope_id VARCHAR(191) NOT NULL,
    created_at DATETIME(3) NOT NULL,
    retired_at DATETIME(3) NULL,
    raw_credential LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, root_incarnation, credential_id, credential_version),
    KEY idx_authority_credential_subject (trust_domain, root_incarnation, subject_id, status),
    KEY idx_authority_credential_scope (trust_domain, credential_scope_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_owner_policy (
    trust_domain VARCHAR(191) NOT NULL,
    root_incarnation VARCHAR(191) NOT NULL,
    policy_id VARCHAR(191) NOT NULL,
    policy_version VARCHAR(191) NOT NULL,
    mode VARCHAR(32) NOT NULL,
    approval_threshold BIGINT UNSIGNED NOT NULL,
    owner_count BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL,
    policy_digest CHAR(64) NOT NULL,
    raw_policy LONGBLOB NOT NULL,
    created_at DATETIME(3) NOT NULL,
    PRIMARY KEY (trust_domain, root_incarnation, policy_id, policy_version),
    UNIQUE KEY uk_authority_owner_policy_digest (trust_domain, policy_digest)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_epoch (
    trust_domain VARCHAR(191) NOT NULL,
    scope_kind VARCHAR(16) NOT NULL,
    scope_id VARCHAR(191) NOT NULL,
    epoch BIGINT UNSIGNED NOT NULL,
    revoked TINYINT(1) NOT NULL DEFAULT 0,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    updated_at DATETIME(3) NOT NULL,
    PRIMARY KEY (trust_domain, scope_kind, scope_id),
    CONSTRAINT chk_authority_epoch_kind CHECK (scope_kind IN ('CONTEXT', 'AUTHORITY', 'CREDENTIAL'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_proof (
    trust_domain VARCHAR(191) NOT NULL,
    proof_id VARCHAR(100) NOT NULL,
    proof_version VARCHAR(64) NOT NULL,
    proof_digest CHAR(64) NOT NULL,
    root_incarnation VARCHAR(191) NOT NULL,
    root_fingerprint CHAR(64) NOT NULL,
    subject_id VARCHAR(191) NOT NULL,
    identity_source_id VARCHAR(191) NOT NULL,
    identity_source_version VARCHAR(191) NOT NULL,
    signer_credential_id VARCHAR(191) NOT NULL,
    signer_credential_version BIGINT UNSIGNED NOT NULL,
    resource VARCHAR(191) NOT NULL,
    action VARCHAR(191) NOT NULL,
    tenant_id VARCHAR(191) NOT NULL,
    operation VARCHAR(191) NOT NULL,
    case_id VARCHAR(191) NOT NULL,
    case_round BIGINT UNSIGNED NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    issued_at DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    registered_at DATETIME(3) NOT NULL,
    raw_proof LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, proof_id),
    UNIQUE KEY uk_authority_proof_digest (trust_domain, proof_digest),
    KEY idx_authority_proof_subject (trust_domain, subject_id),
    KEY idx_authority_proof_target (trust_domain, case_id, case_round)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_proof_scope (
    trust_domain VARCHAR(191) NOT NULL,
    proof_id VARCHAR(100) NOT NULL,
    scope_kind VARCHAR(16) NOT NULL,
    scope_id VARCHAR(191) NOT NULL,
    epoch BIGINT UNSIGNED NOT NULL,
    scope_digest CHAR(64) NOT NULL,
    raw_scope LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, proof_id, scope_kind, scope_id),
    CONSTRAINT chk_authority_proof_scope_kind CHECK (scope_kind IN ('CONTEXT', 'AUTHORITY', 'CREDENTIAL'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_proof_delegation (
    trust_domain VARCHAR(191) NOT NULL,
    proof_id VARCHAR(100) NOT NULL,
    hop_index INT UNSIGNED NOT NULL,
    delegation_id VARCHAR(191) NOT NULL,
    delegation_digest CHAR(64) NOT NULL,
    issuer_subject_id VARCHAR(191) NOT NULL,
    delegate_subject_id VARCHAR(191) NOT NULL,
    raw_delegation LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, proof_id, hop_index),
    UNIQUE KEY uk_authority_proof_delegation_id (trust_domain, proof_id, delegation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_proof_consent (
    trust_domain VARCHAR(191) NOT NULL,
    proof_id VARCHAR(100) NOT NULL,
    consent_id VARCHAR(191) NOT NULL,
    policy_id VARCHAR(191) NOT NULL,
    policy_version VARCHAR(191) NOT NULL,
    consent_digest CHAR(64) NOT NULL,
    threshold BIGINT UNSIGNED NOT NULL,
    approval_count BIGINT UNSIGNED NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL DEFAULT 0,
    raw_consent LONGBLOB NOT NULL,
    PRIMARY KEY (trust_domain, proof_id),
    UNIQUE KEY uk_authority_proof_consent_id (trust_domain, consent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_authority_command (
    trust_domain VARCHAR(191) NOT NULL,
    command_kind VARCHAR(32) NOT NULL,
    command_id VARCHAR(191) NOT NULL,
    command_digest CHAR(64) NOT NULL,
    raw_command LONGBLOB NOT NULL,
    result_scope_kind VARCHAR(16) NOT NULL DEFAULT '',
    result_scope_id VARCHAR(191) NOT NULL DEFAULT '',
    result_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
    result_row_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    result_revoked TINYINT(1) NOT NULL DEFAULT 0,
    result_credential_id VARCHAR(191) NOT NULL DEFAULT '',
    result_credential_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    result_credential_raw LONGBLOB NULL,
    result_digest CHAR(64) NOT NULL,
    completed_at DATETIME(3) NOT NULL,
    PRIMARY KEY (trust_domain, command_id),
    UNIQUE KEY uk_authority_command_digest (trust_domain, command_kind, command_digest),
    KEY idx_authority_command_kind (trust_domain, command_kind),
    CONSTRAINT chk_authority_command_kind CHECK (command_kind IN ('REVOCATION', 'CREDENTIAL_ROTATION'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
