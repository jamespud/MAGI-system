-- MAGI S28: completion/status event provenance. Historical provenance stays 0.
ALTER TABLE magi_event
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0;
