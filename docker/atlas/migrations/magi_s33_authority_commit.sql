-- MAGI S33: one common C2/C3 order and pre-exposure v2 admission write set.
-- Additive to S32. No trust roots, runtime wiring, gate or settlement consumer.
-- Authority sequence and journal_high_watermark are independent counters.
-- All declarations stay blocked. No row in this slice is an exposure permit.

CREATE TABLE IF NOT EXISTS magi_authority_head (
    trust_domain VARCHAR(191) NOT NULL,
    last_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_admission_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_revocation_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_gate_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_settlement_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain),
    CONSTRAINT chk_authority_head_watermarks CHECK (
        last_admission_sequence <= last_sequence AND last_revocation_sequence <= last_sequence
        AND last_gate_sequence <= last_sequence AND last_settlement_sequence <= last_sequence)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_journal_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    journal_id VARCHAR(96) NOT NULL,
    case_id VARCHAR(64) NOT NULL,
    case_round BIGINT UNSIGNED NOT NULL,
    context_manifest_id VARCHAR(191) NOT NULL,
    persona_id VARCHAR(32) NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    high_watermark BIGINT UNSIGNED NOT NULL DEFAULT 0,
    pending_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    row_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    state VARCHAR(32) NOT NULL DEFAULT 'OPEN',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, journal_id),
    UNIQUE KEY uk_ac_journal_identity (trust_domain, case_id, case_round, context_manifest_id, persona_id),
    CONSTRAINT fk_ac_journal_head FOREIGN KEY (trust_domain) REFERENCES magi_authority_head (trust_domain),
    CONSTRAINT chk_ac_journal_pending CHECK (pending_count <= 512),
    CONSTRAINT chk_ac_journal_state CHECK (state = 'OPEN')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_admission_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    unique_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    case_id VARCHAR(64) NOT NULL,
    case_round BIGINT UNSIGNED NOT NULL,
    context_manifest_id VARCHAR(191) NOT NULL,
    persona_id VARCHAR(32) NOT NULL,
    delivery_id VARCHAR(191) NOT NULL,
    provider_request_id VARCHAR(191) NOT NULL,
    journal_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    exact_target_id VARCHAR(191) NOT NULL,
    commit_token VARCHAR(191) NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL,
    journal_high_watermark BIGINT UNSIGNED NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    proof_id VARCHAR(100) NOT NULL,
    admission_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    admission_object_ref TEXT NOT NULL,
    raw_request LONGBLOB NOT NULL,
    owner_job_id VARCHAR(64) NOT NULL,
    owner_worker_id VARCHAR(255) NOT NULL,
    owner_lease_until DATETIME(6) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'ADMITTED',
    exposure_permitted TINYINT(1) NOT NULL DEFAULT 0,
    committed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    UNIQUE KEY uk_ac_admission_key (trust_domain, unique_key),
    UNIQUE KEY uk_ac_admission_identity (trust_domain, case_id, case_round, context_manifest_id, persona_id, delivery_id),
    UNIQUE KEY uk_ac_admission_sequence (trust_domain, authority_sequence),
    UNIQUE KEY uk_ac_admission_hwm (trust_domain, journal_id, journal_high_watermark),
    UNIQUE KEY uk_ac_admission_journal_commit (trust_domain, journal_id, commit_id),
    CONSTRAINT fk_ac_admission_journal FOREIGN KEY (trust_domain, journal_id)
        REFERENCES magi_authority_journal_v2 (trust_domain, journal_id),
    CONSTRAINT chk_ac_admission_sequence CHECK (authority_sequence > 0 AND journal_high_watermark > 0),
    CONSTRAINT chk_ac_admission_state CHECK (state = 'ADMITTED' AND exposure_permitted = 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_journal_event_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    journal_id VARCHAR(96) NOT NULL,
    journal_high_watermark BIGINT UNSIGNED NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    event_id VARCHAR(191) NOT NULL,
    event_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    object_ref TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, journal_id, journal_high_watermark),
    UNIQUE KEY uk_ac_event_commit (trust_domain, commit_id),
    CONSTRAINT fk_ac_event_admission FOREIGN KEY (trust_domain, journal_id, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, journal_id, commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_dependency_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    dependency_id VARCHAR(191) NOT NULL,
    dependency_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    object_ref TEXT NOT NULL,
    ordinal BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id, dependency_id),
    UNIQUE KEY uk_ac_dependency_order (trust_domain, commit_id, ordinal),
    KEY idx_ac_dependency_reverse (trust_domain, dependency_id, dependency_digest),
    CONSTRAINT fk_ac_dependency_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_provider_reservation_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    provider_request_id VARCHAR(191) NOT NULL,
    session_scope_id VARCHAR(191) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'RESERVED',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    UNIQUE KEY uk_ac_reservation_request (trust_domain, provider_request_id),
    CONSTRAINT fk_ac_reservation_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT chk_ac_reservation_state CHECK (state = 'RESERVED')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_exposure_fence_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    fence_id VARCHAR(96) NOT NULL,
    mode VARCHAR(32) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'DECLARED_BLOCKED',
    exposure_permitted TINYINT(1) NOT NULL DEFAULT 0,
    lease_id VARCHAR(191) NOT NULL,
    lease_until DATETIME(6) NOT NULL,
    authority_watermark BIGINT UNSIGNED NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    scoped_epochs LONGBLOB NOT NULL,
    provider_capability_id VARCHAR(191) NOT NULL,
    provider_capability_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    provider_capability_ref TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    UNIQUE KEY uk_ac_fence_id (trust_domain, fence_id),
    CONSTRAINT fk_ac_fence_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT chk_ac_fence_blocked CHECK (state = 'DECLARED_BLOCKED' AND exposure_permitted = 0),
    CONSTRAINT chk_ac_fence_mode CHECK (mode IN ('PROVIDER_ENFORCED_LEASE', 'AUTHORITY_ORDERED_GATE'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_pending_transition_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    journal_id VARCHAR(96) NOT NULL,
    expected_pending BIGINT UNSIGNED NOT NULL,
    next_pending BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    CONSTRAINT fk_ac_pending_admission FOREIGN KEY (trust_domain, journal_id, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, journal_id, commit_id),
    CONSTRAINT chk_ac_pending_admit CHECK (expected_pending < 512 AND next_pending = expected_pending + 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_unique_binding_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    unique_key CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    exact_target_id VARCHAR(191) NOT NULL,
    commit_token VARCHAR(191) NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL,
    journal_high_watermark BIGINT UNSIGNED NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, unique_key),
    UNIQUE KEY uk_ac_binding_commit (trust_domain, commit_id),
    CONSTRAINT fk_ac_binding_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT chk_ac_binding_sequence CHECK (authority_sequence > 0 AND journal_high_watermark > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_token_consumed_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_token VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    consumed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_token),
    UNIQUE KEY uk_ac_token_commit (trust_domain, commit_id),
    CONSTRAINT fk_ac_token_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_lineage_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    lineage_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL,
    journal_high_watermark BIGINT UNSIGNED NOT NULL,
    raw_lineage LONGBLOB NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    UNIQUE KEY uk_ac_lineage_digest (trust_domain, lineage_digest),
    CONSTRAINT fk_ac_lineage_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_outbox_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_id VARCHAR(96) NOT NULL,
    event_kind VARCHAR(32) NOT NULL DEFAULT 'ADMISSION_COMMITTED',
    payload LONGBLOB NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'BLOCKED',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_id),
    CONSTRAINT fk_ac_outbox_admission FOREIGN KEY (trust_domain, commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT chk_ac_outbox_blocked CHECK (event_kind = 'ADMISSION_COMMITTED' AND state = 'BLOCKED')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
