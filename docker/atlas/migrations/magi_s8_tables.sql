-- MAGI S8: 9 remaining tables (atlas migration for MySQL production)
-- Closes §23: relational normalization of nested entities currently in JSON TEXT.
-- Style matches s6/s7: VARCHAR(64) IDs, TEXT for JSON, ENGINE=InnoDB CHARSET=utf8mb4.

-- 1. magi_config: MagiConfig persistence (per-agent execution strategy)
CREATE TABLE IF NOT EXISTS magi_config (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    code VARCHAR(32) NOT NULL DEFAULT '',
    name VARCHAR(128) NOT NULL DEFAULT '',
    persona TEXT NOT NULL DEFAULT '',
    persona_def_json TEXT NOT NULL DEFAULT '',
    objective_json TEXT NOT NULL DEFAULT '',
    risk_tendency VARCHAR(32) NOT NULL DEFAULT '',
    risk_policy_json TEXT NOT NULL DEFAULT '',
    evidence_standard_json TEXT NOT NULL DEFAULT '',
    model_ref_json TEXT NOT NULL DEFAULT '',
    tools_json TEXT NOT NULL DEFAULT '',
    loop_policy_json TEXT NOT NULL DEFAULT '',
    reflection_policy_json TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 2. decision_task: normalized DecisionTask (was in decision_case.task_json)
CREATE TABLE IF NOT EXISTS decision_task (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    canonical_question TEXT NOT NULL DEFAULT '',
    decision_type VARCHAR(32) NOT NULL DEFAULT '',
    background TEXT NOT NULL DEFAULT '',
    constraints_json TEXT NOT NULL DEFAULT '',
    dimensions_json TEXT NOT NULL DEFAULT '',
    information_needs_json TEXT NOT NULL DEFAULT '',
    success_criteria_json TEXT NOT NULL DEFAULT '',
    unknowns_json TEXT NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 3. magi_agent_checkpoint: AgentState for checkpoint/resume (§18)
CREATE TABLE IF NOT EXISTS magi_agent_checkpoint (
    run_id VARCHAR(64) NOT NULL PRIMARY KEY,
    messages_json MEDIUMTEXT NOT NULL DEFAULT '',
    messages_ref_json TEXT NOT NULL DEFAULT '',
    step_count INT NOT NULL DEFAULT 0,
    token_used INT NOT NULL DEFAULT 0,
    phase VARCHAR(32) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 4. magi_tool_call: ToolCallRecord (was only in agent loop trace)
CREATE TABLE IF NOT EXISTS magi_tool_call (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    agent_run_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_call_id VARCHAR(64) NOT NULL DEFAULT '',
    tool_name VARCHAR(128) NOT NULL DEFAULT '',
    arguments TEXT NOT NULL DEFAULT '',
    valid TINYINT(1) NOT NULL DEFAULT 0,
    result TEXT NOT NULL DEFAULT '',
    err TEXT NOT NULL DEFAULT '',
    evidence_id VARCHAR(64) NOT NULL DEFAULT '',
    duration_ms INT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 5. claim_evidence_relation: Claim->Evidence supports (was JSON in claim.supports_json)
CREATE TABLE IF NOT EXISTS claim_evidence_relation (
    claim_id VARCHAR(64) NOT NULL,
    evidence_id VARCHAR(64) NOT NULL,
    relation_type VARCHAR(32) NOT NULL DEFAULT 'supports',
    PRIMARY KEY (claim_id, evidence_id, relation_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 6. claim_relation: Claim->Claim contradicts (was JSON in claim.contradicts_json)
CREATE TABLE IF NOT EXISTS claim_relation (
    claim_id_a VARCHAR(64) NOT NULL,
    claim_id_b VARCHAR(64) NOT NULL,
    relation_type VARCHAR(32) NOT NULL DEFAULT 'contradicts',
    PRIMARY KEY (claim_id_a, claim_id_b, relation_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 7. evidence_summary: EvidenceSummary (was only in-memory on LoopResult)
CREATE TABLE IF NOT EXISTS evidence_summary (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    agent_run_id VARCHAR(64) NOT NULL DEFAULT '',
    evidence_by_type_json TEXT NOT NULL DEFAULT '',
    claims_json TEXT NOT NULL DEFAULT '',
    ready TINYINT(1) NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 8. vote_utility_score: Vote.UtilityScores normalized (was JSON in vote.utility_scores_json)
CREATE TABLE IF NOT EXISTS vote_utility_score (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    vote_id VARCHAR(64) NOT NULL,
    dimension_code VARCHAR(64) NOT NULL DEFAULT '',
    score FLOAT NOT NULL DEFAULT 0,
    evidence_ids_json TEXT NOT NULL DEFAULT '',
    claim_ids_json TEXT NOT NULL DEFAULT '',
    reasoning TEXT NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 9. magi_evaluation: Evaluation metrics persistence (was only on Resolution in-memory)
CREATE TABLE IF NOT EXISTS magi_evaluation (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    tool_success_rate FLOAT NOT NULL DEFAULT 0,
    avg_tool_calls FLOAT NOT NULL DEFAULT 0,
    tool_param_failures INT NOT NULL DEFAULT 0,
    evidence_count INT NOT NULL DEFAULT 0,
    avg_reliability FLOAT NOT NULL DEFAULT 0,
    unique_source_types INT NOT NULL DEFAULT 0,
    gate_failures INT NOT NULL DEFAULT 0,
    max_steps_exceeded INT NOT NULL DEFAULT 0,
    validation_failures INT NOT NULL DEFAULT 0,
    first_round_consensus TINYINT(1) NOT NULL DEFAULT 0,
    consensus_round INT NOT NULL DEFAULT 0,
    consensus_outcome VARCHAR(64) NOT NULL DEFAULT '',
    total_tokens BIGINT NOT NULL DEFAULT 0,
    avg_tokens_per_agent FLOAT NOT NULL DEFAULT 0,
    total_steps INT NOT NULL DEFAULT 0,
    total_tool_calls INT NOT NULL DEFAULT 0,
    counterfactual_stability FLOAT NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
