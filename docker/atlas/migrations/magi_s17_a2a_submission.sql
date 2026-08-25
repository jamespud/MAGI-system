-- MAGI S17: idempotent inbound A2A submission bindings.
-- This table deliberately has no cascading foreign keys. A decision case is
-- an audit record and must survive later retention of its conversation turns.

CREATE TABLE IF NOT EXISTS a2a_submission (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    message_id VARCHAR(128) NOT NULL,
    request_hash VARCHAR(128) NOT NULL,
    task_id VARCHAR(64) NOT NULL,
    context_id VARCHAR(64) NOT NULL DEFAULT '',
    input_message_id VARCHAR(64) NOT NULL DEFAULT '',
    case_message_id VARCHAR(64) NOT NULL DEFAULT '',
    state VARCHAR(16) NOT NULL,
    error_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uq_a2a_submission_user_message (user_id, message_id),
    UNIQUE KEY uq_a2a_submission_task (task_id),
    KEY idx_a2a_submission_state (state, created_at),
    KEY idx_a2a_submission_context (context_id),
    KEY idx_a2a_submission_input_message (input_message_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
