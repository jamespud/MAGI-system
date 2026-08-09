-- MAGI S12: distributed lock for the recurring scheduler (multi-instance)

CREATE TABLE IF NOT EXISTS magi_scheduler_lock (
    name VARCHAR(64) NOT NULL PRIMARY KEY,
    owner VARCHAR(128) NOT NULL DEFAULT '',
    lease_until DATETIME NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
