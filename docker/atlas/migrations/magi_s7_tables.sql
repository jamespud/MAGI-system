-- MAGI S7: 3 additional tables (debate_round, reflection, case_memory_projection)

CREATE TABLE IF NOT EXISTS debate_round (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    round INT NOT NULL DEFAULT 0,
    packet_json TEXT NOT NULL DEFAULT '',
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME NULL,
    INDEX idx_debate_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS reflection (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    agent_run_id VARCHAR(64) NOT NULL,
    round INT NOT NULL DEFAULT 0,
    previous_vote_id VARCHAR(64) NOT NULL DEFAULT '',
    position_change VARCHAR(32) NOT NULL DEFAULT 'maintain',
    accepted_claims_json TEXT NOT NULL DEFAULT '',
    rejected_claims_json TEXT NOT NULL DEFAULT '',
    new_evidence_ids_json TEXT NOT NULL DEFAULT '',
    reasoning TEXT NOT NULL DEFAULT '',
    ready_to_revote BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_reflection_run (agent_run_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS case_memory_projection (
    case_id VARCHAR(64) NOT NULL PRIMARY KEY,
    question_summary TEXT NOT NULL DEFAULT '',
    context_summary TEXT NOT NULL DEFAULT '',
    key_evidence_json TEXT NOT NULL DEFAULT '',
    key_claims_json TEXT NOT NULL DEFAULT '',
    votes_json TEXT NOT NULL DEFAULT '',
    resolution TEXT NOT NULL DEFAULT '',
    outcome_json TEXT NOT NULL DEFAULT '',
    projection_version INT NOT NULL DEFAULT 1
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
