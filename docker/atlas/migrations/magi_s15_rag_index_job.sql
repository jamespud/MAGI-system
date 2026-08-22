-- MAGI S15: durable RAG index job queue (mirrors decision_job; worker claims
-- jobs with a lease, heartbeat keeps the lease alive, expired leases are
-- requeued by the poller). Mirrors adapter/model.go RagIndexJobModel.

CREATE TABLE IF NOT EXISTS rag_index_job (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    kind VARCHAR(16) NOT NULL DEFAULT 'index',
    source VARCHAR(32) NOT NULL DEFAULT '',
    source_ref VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'queued',
    attempt INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 3,
    worker_id VARCHAR(128) NOT NULL DEFAULT '',
    lease_until DATETIME NULL,
    available_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_runnable (status, available_at),
    INDEX idx_source_ref (source_ref)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
