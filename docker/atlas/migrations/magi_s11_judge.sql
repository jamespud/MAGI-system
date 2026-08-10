-- MAGI S11: LLM-as-a-Judge evaluations for completed cases

CREATE TABLE IF NOT EXISTS magi_judge_eval (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL UNIQUE,
    report_quality FLOAT NOT NULL DEFAULT 0,
    evidence_consistency FLOAT NOT NULL DEFAULT 0,
    reflection_validity FLOAT NOT NULL DEFAULT 0,
    overall FLOAT NOT NULL DEFAULT 0,
    rationale TEXT NOT NULL DEFAULT '',
    model_name VARCHAR(128) NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE INDEX idx_judge_case (case_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
