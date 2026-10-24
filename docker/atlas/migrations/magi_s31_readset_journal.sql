-- MAGI S31: durable local ReadSetJournal pre-exposure registration (#44).
--
-- This migration is NOT an implementation of the #45 AuthorityContract and is
-- NOT wire-compatible with its JOURNAL_ADMIT CompleteWritePlan. #45 describes
-- an eventual post-policy settlement write; it does not define the separate
-- append-before-provider-exposure transaction required here. That missing
-- transaction is tracked as a #45/#47 Design Change in
-- docs/readset-journal-c1-c2b.md.
--
-- The application slice can durably register an exact canonical event before a
-- caller exposes material, then classify a delivery as PENDING/UNKNOWN/
-- UNVERIFIABLE. It cannot write APPLIED_VERIFIED or FAILED: authority_sequence,
-- verification_record_json, injected_at and verified_at remain NULL until a
-- future independently trusted provider/C2/C3 path writes them under the
-- approved contract.
--
-- Rollback warning: stop writers and preserve/export these tables before
-- dropping them. Dropping the journal erases durable pre-exposure facts and
-- cannot be treated as "no exposure happened". A generation-unaware or
-- #45-incompatible writer must never share these tables.

CREATE TABLE readset_journal (
    id VARCHAR(96) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    round_no BIGINT UNSIGNED NOT NULL,
    context_manifest_id VARCHAR(191) NOT NULL,
    persona_id VARCHAR(32) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'OPEN',
    high_watermark BIGINT UNSIGNED NOT NULL DEFAULT 0,
    pending_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    event_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    source_dependency_index_version VARCHAR(191) NOT NULL,
    admission_scope_id VARCHAR(191) NOT NULL,
    context_revocation_scope_id VARCHAR(191) NOT NULL,
    context_revocation_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
    authority_scope_id VARCHAR(191) NOT NULL,
    authority_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
    credential_scope_id VARCHAR(191) NOT NULL,
    credential_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
    owner_job_id VARCHAR(64) NOT NULL,
    owner_worker_id VARCHAR(255) NOT NULL,
    execution_generation BIGINT UNSIGNED NOT NULL,
    owner_lease_until DATETIME(6) NOT NULL,
    state_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_readset_journal_key (case_id, round_no, context_manifest_id, persona_id),
    KEY idx_readset_journal_generation (case_id, execution_generation),
    CONSTRAINT chk_readset_journal_generation CHECK (execution_generation > 0),
    CONSTRAINT chk_readset_journal_state CHECK (state IN ('OPEN', 'QUIESCING', 'INPUT_CLOSED')),
    CONSTRAINT chk_readset_journal_hwm CHECK (high_watermark <= 512),
    CONSTRAINT chk_readset_journal_pending CHECK (pending_count <= high_watermark),
    CONSTRAINT chk_readset_journal_bytes CHECK (event_bytes <= 16777216)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE readset_journal_event (
    journal_id VARCHAR(96) NOT NULL,
    local_sequence BIGINT UNSIGNED NOT NULL,
    event_version VARCHAR(16) NOT NULL,
    delivery_id VARCHAR(191) NOT NULL,
    provider_request_id VARCHAR(191) NOT NULL,
    registration_state VARCHAR(32) NOT NULL DEFAULT 'REGISTERED',
    execution_generation BIGINT UNSIGNED NOT NULL,
    owner_job_id VARCHAR(64) NOT NULL,
    owner_worker_id VARCHAR(255) NOT NULL,
    owner_lease_until DATETIME(6) NOT NULL,
    source_node_id VARCHAR(191) NOT NULL,
    source_version VARCHAR(191) NOT NULL,
    target_persona VARCHAR(32) NOT NULL,
    payload_json LONGBLOB NOT NULL,
    payload_digest CHAR(64) NOT NULL,
    request_digest CHAR(64) NOT NULL,
    payload_bytes INT UNSIGNED NOT NULL,
    registered_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (journal_id, local_sequence),
    UNIQUE KEY uq_readset_event_delivery (journal_id, delivery_id),
    UNIQUE KEY uq_readset_event_provider_request (journal_id, provider_request_id),
    KEY idx_readset_event_provider_request (provider_request_id),
    CONSTRAINT fk_readset_event_journal FOREIGN KEY (journal_id) REFERENCES readset_journal (id),
    CONSTRAINT chk_readset_event_sequence CHECK (local_sequence > 0),
    CONSTRAINT chk_readset_event_generation CHECK (execution_generation > 0),
    CONSTRAINT chk_readset_event_state CHECK (registration_state = 'REGISTERED'),
    CONSTRAINT chk_readset_event_size CHECK (payload_bytes > 0 AND payload_bytes <= 32768)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE readset_journal_delivery (
    journal_id VARCHAR(96) NOT NULL,
    delivery_id VARCHAR(191) NOT NULL,
    event_sequence BIGINT UNSIGNED NOT NULL,
    provider_request_id VARCHAR(191) NOT NULL,
    state VARCHAR(32) NOT NULL DEFAULT 'PENDING',
    state_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    first_execution_generation BIGINT UNSIGNED NOT NULL,
    last_execution_generation BIGINT UNSIGNED NOT NULL,
    last_owner_job_id VARCHAR(64) NOT NULL,
    last_owner_worker_id VARCHAR(255) NOT NULL,
    last_owner_lease_until DATETIME(6) NOT NULL,
    settled_execution_generation BIGINT UNSIGNED NULL,
    payload_digest CHAR(64) NOT NULL,
    authority_sequence BIGINT UNSIGNED NULL,
    verification_record_json LONGBLOB NULL,
    injected_at DATETIME(6) NULL,
    verified_at DATETIME(6) NULL,
    last_reason TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (journal_id, delivery_id),
    UNIQUE KEY uq_readset_delivery_provider_request (journal_id, provider_request_id),
    UNIQUE KEY uq_readset_delivery_event (journal_id, event_sequence),
    KEY idx_readset_delivery_state (journal_id, state, delivery_id),
    CONSTRAINT fk_readset_delivery_event FOREIGN KEY (journal_id, event_sequence)
        REFERENCES readset_journal_event (journal_id, local_sequence),
    CONSTRAINT chk_readset_delivery_generation CHECK (first_execution_generation > 0 AND last_execution_generation > 0),
    CONSTRAINT chk_readset_delivery_state CHECK (state IN ('PENDING', 'APPLIED_VERIFIED', 'FAILED', 'UNKNOWN', 'UNVERIFIABLE')),
    CONSTRAINT chk_readset_delivery_applied CHECK (
        state <> 'APPLIED_VERIFIED' OR
        (authority_sequence IS NOT NULL AND authority_sequence > 0 AND
         verification_record_json IS NOT NULL AND injected_at IS NOT NULL AND verified_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE readset_journal_node (
    journal_id VARCHAR(96) NOT NULL,
    node_id VARCHAR(191) NOT NULL,
    source_entry_id VARCHAR(191) NOT NULL,
    source_version VARCHAR(191) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    node_digest CHAR(64) NOT NULL,
    node_json LONGBLOB NOT NULL,
    first_sequence BIGINT UNSIGNED NOT NULL,
    admission_generation BIGINT UNSIGNED NOT NULL,
    admission_job_id VARCHAR(64) NOT NULL,
    admission_worker_id VARCHAR(255) NOT NULL,
    admission_scope_id VARCHAR(191) NOT NULL,
    admission_authority_sequence BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (journal_id, node_id),
    UNIQUE KEY uq_readset_node_source_version (journal_id, source_entry_id, source_version),
    KEY idx_readset_node_first_sequence (journal_id, first_sequence),
    CONSTRAINT fk_readset_node_journal FOREIGN KEY (journal_id) REFERENCES readset_journal (id),
    CONSTRAINT chk_readset_node_generation CHECK (admission_generation > 0),
    CONSTRAINT chk_readset_node_kind CHECK (kind IN ('RAW', 'DERIVED', 'EMPTY_RESULT'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE readset_journal_edge (
    journal_id VARCHAR(96) NOT NULL,
    child_node_id VARCHAR(191) NOT NULL,
    parent_node_id VARCHAR(191) NOT NULL,
    created_sequence BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (journal_id, child_node_id, parent_node_id),
    KEY idx_readset_edge_reverse (journal_id, parent_node_id, child_node_id),
    CONSTRAINT fk_readset_edge_journal FOREIGN KEY (journal_id) REFERENCES readset_journal (id),
    CONSTRAINT fk_readset_edge_child FOREIGN KEY (journal_id, child_node_id)
        REFERENCES readset_journal_node (journal_id, node_id),
    CONSTRAINT fk_readset_edge_parent FOREIGN KEY (journal_id, parent_node_id)
        REFERENCES readset_journal_node (journal_id, node_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
