-- MAGI S13: per-user per-tool rate-limit counters (multi-instance safe)

CREATE TABLE IF NOT EXISTS magi_tool_quota_counter (
    user_id BIGINT NOT NULL,
    tool_name VARCHAR(128) NOT NULL,
    window_start DATETIME NOT NULL,
    calls INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, tool_name, window_start)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
