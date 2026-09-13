-- MAGI S23: widen the runtime identity columns.
--
-- Production run identifiers are "case-<uuid>-<role>-r<round>-<phase>" (41 +
-- role + phase), and the dispatcher's retry path appends "-a<attempt>" to the
-- execution id and "-retryN" to the checkpoint id, so they legitimately exceed
-- the 64 characters S21 published:
--
--   melchior  checkpoint 65, execution 68
--   balthasar checkpoint 66, execution 69
--   casper    checkpoint 63, execution 66
--
-- The physical attempt id is the same string (traceIdentity), so
-- runtime_invocation_attempt.attempt_id overflowed too. On MySQL that surfaced
-- as Error 1406 "Data too long for column 'run_id'", failing every run; SQLite
-- ignores VARCHAR widths, and the Go tests used short case ids, so this was
-- invisible until a real case id ran against MySQL.
--
-- 191 is the project's indexable UTF-8 limit (utf8mb4 x 191 = 764 bytes), the
-- same width other indexed identity columns use, and it is mirrored by
-- validation.MaxInvocationRunIDBytes. step_id is deliberately NOT widened: it is
-- a sha256 hex digest (execution.NewStepID), always exactly 64 characters, so
-- varchar(64) is already correct for it and widening it here would make an
-- Atlas-provisioned schema diverge from the AutoMigrate path.
--
-- This is also the forward-only path for databases that already applied the
-- earlier (64-wide) revision of S21, so an existing deployment does not depend
-- on editing an already-applied migration.

ALTER TABLE runtime_invocation
    MODIFY run_id VARCHAR(191) NOT NULL;

ALTER TABLE runtime_invocation_attempt
    MODIFY attempt_id VARCHAR(191) NOT NULL;
