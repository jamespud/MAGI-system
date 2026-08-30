-- MAGI S21: durable runtime kernel invocation and physical attempt records.
-- invocation_id identifies the logical operation; attempt_id belongs only to
-- the separately persisted physical execution attempt.

CREATE TABLE IF NOT EXISTS runtime_invocation (
    invocation_id VARCHAR(64) NOT NULL PRIMARY KEY,

    run_id VARCHAR(64) NOT NULL,
    step_id VARCHAR(64) NOT NULL,

    kind VARCHAR(32) NOT NULL,

    logical_ordinal INT NOT NULL,

    status VARCHAR(32) NOT NULL,

    attempt_count INT NOT NULL DEFAULT 0,

    operation_name VARCHAR(128) NOT NULL DEFAULT '',

    retry_safety VARCHAR(32) NOT NULL,

    idempotency_key VARCHAR(128) NULL,

    input_digest VARCHAR(64) NOT NULL DEFAULT '',

    input_json MEDIUMTEXT NOT NULL,

    output_json MEDIUMTEXT NULL,

    error TEXT NULL,

    started_at DATETIME NULL,
    completed_at DATETIME NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_runtime_idempotency (idempotency_key),
    KEY idx_runtime_run_step (run_id, step_id),
    KEY idx_runtime_run_status (run_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS runtime_invocation_attempt (
    attempt_id VARCHAR(64) NOT NULL PRIMARY KEY,
    invocation_id VARCHAR(64) NOT NULL,
    attempt_no INT NOT NULL,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL,
    started_at DATETIME NULL,
    completed_at DATETIME NULL,
    error TEXT NULL,

    UNIQUE KEY uk_runtime_invocation_attempt_no (invocation_id, attempt_no),
    KEY idx_runtime_attempt_invocation (invocation_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
