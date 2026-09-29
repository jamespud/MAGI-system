-- MAGI S25: persist the Case execution generation.
--
-- A Case needs one durable, never-decreasing ownership epoch so that claim,
-- artifact writes, status transitions, terminal commits and cleanup can all be
-- fenced by the same predicate. The existing identifiers cannot serve that
-- purpose: decision_job.attempt is reset when a failed/cancelled/paused job is
-- re-admitted, and the runtime DecisionCase.ExecutionAttempt is not persisted.
--
-- execution_generation starts at 0, which means "legacy / unknown provenance".
-- Existing rows are deliberately NOT backfilled to 1 and no generation is
-- inferred from the "-aN-" segment some artifact ids carry: inventing a
-- provenance for historical data would make the fence unauditable. The first
-- generation-aware claim produces generation 1.
--
-- decision_job.execution_generation records which Case generation a claim owns;
-- this migration only persists the column and never advances it.
--
-- Rollback note: once a later task writes artifacts under a positive
-- generation, rolling back to a generation-unaware writer is unsafe, because
-- that writer cannot distinguish generations when it writes, transitions or
-- cleans up.
ALTER TABLE decision_case
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE decision_job
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0;
