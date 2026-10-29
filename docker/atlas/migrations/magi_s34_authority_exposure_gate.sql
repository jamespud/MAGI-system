CREATE TABLE IF NOT EXISTS magi_authority_exposure_gate_attempt_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    attempt_id VARCHAR(191) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    raw_request LONGBLOB NOT NULL,
    commit_token VARCHAR(191) NOT NULL,
    outcome VARCHAR(32) NOT NULL,
    code VARCHAR(96) NOT NULL,
    blocked_reason TEXT NULL,
    gate_id VARCHAR(191) NOT NULL,
    admission_commit_id VARCHAR(96) NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    exposure_permitted TINYINT(1) NOT NULL DEFAULT 0,
    committed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, attempt_id),
    UNIQUE KEY uk_aeg_attempt_request (trust_domain, attempt_id, request_digest),
    CONSTRAINT fk_aeg_attempt_admission FOREIGN KEY (trust_domain, admission_commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT chk_aeg_attempt_outcome CHECK (
        (outcome = 'BLOCKED' AND authority_sequence = 0 AND exposure_permitted = 0 AND blocked_reason IS NOT NULL)
        OR (outcome = 'EXPOSURE_GATED' AND authority_sequence > 0 AND exposure_permitted = 0 AND blocked_reason IS NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_exposure_gate_commit_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    gate_id VARCHAR(191) NOT NULL,
    gate_commit_id VARCHAR(96) NOT NULL,
    attempt_id VARCHAR(191) NOT NULL,
    admission_commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL,
    exposure_permitted TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, gate_id),
    UNIQUE KEY uk_aeg_commit_id (trust_domain, gate_commit_id),
    UNIQUE KEY uk_aeg_commit_attempt (trust_domain, attempt_id),
    UNIQUE KEY uk_aeg_commit_sequence (trust_domain, authority_sequence),
    CONSTRAINT fk_aeg_commit_admission FOREIGN KEY (trust_domain, admission_commit_id)
        REFERENCES magi_authority_admission_v2 (trust_domain, commit_id),
    CONSTRAINT fk_aeg_commit_attempt FOREIGN KEY (trust_domain, attempt_id)
        REFERENCES magi_authority_exposure_gate_attempt_v2 (trust_domain, attempt_id),
    CONSTRAINT chk_aeg_commit_sequence CHECK (authority_sequence > 0 AND exposure_permitted = 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_exposure_gate_lineage_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    gate_commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    authority_sequence BIGINT UNSIGNED NOT NULL,
    raw_lineage LONGBLOB NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, gate_commit_id),
    UNIQUE KEY uk_aeg_lineage_sequence (trust_domain, authority_sequence),
    CONSTRAINT fk_aeg_lineage_commit FOREIGN KEY (trust_domain, gate_commit_id)
        REFERENCES magi_authority_exposure_gate_commit_v2 (trust_domain, gate_commit_id),
    CONSTRAINT chk_aeg_lineage_sequence CHECK (authority_sequence > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_exposure_gate_token_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    commit_token VARCHAR(191) NOT NULL,
    gate_commit_id VARCHAR(96) NOT NULL,
    request_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    consumed_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, commit_token),
    UNIQUE KEY uk_aeg_token_commit (trust_domain, gate_commit_id),
    CONSTRAINT fk_aeg_token_commit FOREIGN KEY (trust_domain, gate_commit_id)
        REFERENCES magi_authority_exposure_gate_commit_v2 (trust_domain, gate_commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE IF NOT EXISTS magi_authority_exposure_gate_attestation_v2 (
    trust_domain VARCHAR(191) NOT NULL,
    gate_commit_id VARCHAR(96) NOT NULL,
    capability_id VARCHAR(191) NOT NULL,
    capability_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    verification_id VARCHAR(191) NOT NULL,
    verification_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    witness_id VARCHAR(191) NOT NULL,
    witness_evidence_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    valid_until DATETIME(6) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (trust_domain, gate_commit_id),
    CONSTRAINT fk_aeg_attestation_commit FOREIGN KEY (trust_domain, gate_commit_id)
        REFERENCES magi_authority_exposure_gate_commit_v2 (trust_domain, gate_commit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
