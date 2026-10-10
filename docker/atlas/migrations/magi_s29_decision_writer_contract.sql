-- T6 singleton control is BLOCKED on installation. Runtime has SELECT only.
CREATE TABLE decision_writer_contract (
    id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
    contract_version INT UNSIGNED NOT NULL DEFAULT 1,
    admission_state VARCHAR(16) NOT NULL DEFAULT 'BLOCKED',
    cutover_epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,
    legacy_rollback_forbidden TINYINT(1) NOT NULL DEFAULT 0,
    writer_account VARCHAR(255) NOT NULL DEFAULT '',
    legacy_account VARCHAR(255) NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Existing deployments may already have written positive generations. Never
-- infer rollback permission from currently retained rows. Fresh init is separate.
INSERT INTO decision_writer_contract (id, legacy_rollback_forbidden) VALUES (1, 1);
