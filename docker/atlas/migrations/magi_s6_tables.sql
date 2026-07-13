-- MAGI S6: 7 core tables (atlas migration for MySQL production)
-- Complex/nested fields stored as JSON TEXT columns.

CREATE TABLE IF NOT EXISTS decision_case (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL DEFAULT 0,
    question TEXT NOT NULL DEFAULT '',
    context TEXT NOT NULL DEFAULT '',
    constraints_json TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'DRAFT',
    current_phase VARCHAR(32) NOT NULL DEFAULT '',
    max_debate_rounds INT NOT NULL DEFAULT 0,
    deadline DATETIME NULL,
    task_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_agent_run (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    magi_config_id VARCHAR(64) NOT NULL DEFAULT '',
    magi_code VARCHAR(32) NOT NULL DEFAULT '',
    round INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'running',
    usage_json TEXT NOT NULL DEFAULT '',
    err TEXT NOT NULL DEFAULT '',
    checkpoint_json TEXT NOT NULL DEFAULT '',
    summary_json TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME NULL,
    INDEX idx_agent_run_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS evidence_record (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    agent_run_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_call_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_name VARCHAR(128) NOT NULL DEFAULT '',
    source_type VARCHAR(32) NOT NULL DEFAULT '',
    source_uri VARCHAR(512) NOT NULL DEFAULT '',
    raw_content TEXT NOT NULL DEFAULT '',
    observation TEXT NOT NULL DEFAULT '',
    reliability_json TEXT NOT NULL DEFAULT '',
    collected_by VARCHAR(32) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_evidence_case (case_id),
    INDEX idx_evidence_run (agent_run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS claim (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    agent_run_id VARCHAR(64) NOT NULL DEFAULT '',
    statement TEXT NOT NULL DEFAULT '',
    supports_json TEXT NOT NULL DEFAULT '',
    contradicts_json TEXT NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'open',
    created_by VARCHAR(32) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_claim_case (case_id),
    INDEX idx_claim_run (agent_run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_vote (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    agent_run_id VARCHAR(64) NOT NULL DEFAULT '',
    round INT NOT NULL DEFAULT 0,
    decision VARCHAR(32) NOT NULL DEFAULT '',
    confidence DOUBLE NOT NULL DEFAULT 0,
    utility_scores_json TEXT NOT NULL DEFAULT '',
    key_claim_ids_json TEXT NOT NULL DEFAULT '',
    evidence_ids_json TEXT NOT NULL DEFAULT '',
    reasoning_summary TEXT NOT NULL DEFAULT '',
    conditions_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_vote_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS resolution (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    consensus_json TEXT NOT NULL DEFAULT '',
    final_decision VARCHAR(32) NOT NULL DEFAULT '',
    final_report TEXT NOT NULL DEFAULT '',
    key_evidence_ids_json TEXT NOT NULL DEFAULT '',
    key_claim_ids_json TEXT NOT NULL DEFAULT '',
    vote_ids_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_resolution_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS magi_event (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    run_id VARCHAR(64) NOT NULL DEFAULT '',
    agent_code VARCHAR(32) NOT NULL DEFAULT '',
    type VARCHAR(64) NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '',
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_event_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
