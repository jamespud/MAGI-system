-- MAGI S26: durable Claim reply-loss recovery identity.
--
-- claim_token identifies one Claim operation so a caller that loses the commit
-- reply can re-read the exact row/generation instead of blindly allocating a
-- second generation. It is NOT a fencing generation, lease, or authorization
-- credential and must never substitute for execution_generation.
--
-- Legacy rows remain NULL. No token is synthesized from worker_id, attempt,
-- lease timestamps, or artifact identifiers.
ALTER TABLE decision_job
    ADD COLUMN claim_token VARCHAR(36) NULL DEFAULT NULL;

CREATE UNIQUE INDEX uk_decision_job_claim_token
    ON decision_job (claim_token);

-- A single decision_job row is reused across generations, so its current
-- claim_token is necessarily overwritten. This append-only registry preserves
-- the immutable token -> committed Claim mapping and prevents a superseded
-- token from ever being reallocated to a later generation.
CREATE TABLE decision_job_claim (
    claim_token VARCHAR(36) NOT NULL PRIMARY KEY,
    job_id VARCHAR(64) NOT NULL,
    case_id VARCHAR(64) NOT NULL,
    worker_id VARCHAR(255) NOT NULL,
    execution_generation BIGINT NOT NULL,
    attempt BIGINT NOT NULL,
    max_attempts BIGINT NOT NULL,
    lease_until DATETIME(3) NOT NULL,
    claimed_at DATETIME(3) NOT NULL,
    INDEX idx_decision_job_claim_job (job_id),
    INDEX idx_decision_job_claim_case_generation (case_id, execution_generation)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Downgrade warning: after any non-NULL claim_token / registry row exists,
-- reverting to a writer that does not understand claim-token idempotency can
-- replay an ambiguous Claim and allocate an extra execution generation. A
-- rollback therefore requires stopping generation-aware writers first and is
-- not a live mixed-version downgrade.
