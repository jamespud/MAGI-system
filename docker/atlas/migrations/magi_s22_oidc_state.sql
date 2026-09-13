-- S22: one-time OIDC authorization states, shared across replicas.
--
-- The login flow used to keep pending states in process memory, so a provider
-- callback that landed on a different replica than /login always failed with
-- "invalid or expired state" (see docs/reliability-hazard-audit.md §3.5).
CREATE TABLE IF NOT EXISTS oidc_auth_state (
    state VARCHAR(64) NOT NULL PRIMARY KEY,
    expires_at DATETIME(3) NOT NULL,
    consumed_at DATETIME(3) NULL,
    KEY idx_oidc_state_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
