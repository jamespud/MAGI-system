-- MAGI S24: bind a human tool approval to one exact logical invocation.
--
-- S10 keyed approval requests by (case_id, run_id, tool_name) only, and the
-- agent loop reused whatever decision that key returned. Approving a tool once
-- therefore also authorized a later call to the same tool with different
-- arguments in the same run, and a second call with identical arguments was
-- indistinguishable from a retry of the first (issue #9).
--
-- The authoritative identity is now the logical invocation
-- (execution.NewInvocationID: sha256 over the step id, the invocation kind and
-- the ordinal, always exactly 64 hex characters). It is stable across a
-- dispatcher retry or a resume of the same logical call, and distinct for a
-- different call even when tool and arguments are identical, so the unique key
-- is (case_id, invocation_id). intent_digest additionally pins the tool and its
-- canonical arguments; the runtime validates it on reuse.
--
-- invocation_id is NULL-able on purpose. Rows persisted before this revision
-- have no invocation, and MySQL allows repeated NULLs in a unique index, so
-- existing rows cannot collide with each other during the upgrade. The
-- repository refuses an empty invocation lookup, so a legacy row is never
-- reused by a new call.
--
-- Both the startup AutoMigrate path and this forward-only script produce the
-- same shape; apply this once, per deployment, when provisioning with Atlas
-- instead of the service.
ALTER TABLE magi_approval_request
    ADD COLUMN invocation_id VARCHAR(64) NULL;
ALTER TABLE magi_approval_request
    ADD COLUMN intent_digest VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE magi_approval_request
    ADD UNIQUE KEY uk_approval_invocation (case_id, invocation_id);
