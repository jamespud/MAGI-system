-- MAGI S24: bind a human tool approval to one exact invocation intent.
--
-- S10 keyed approval requests by (case_id, run_id, tool_name) only, and the
-- agent loop reused whatever decision that key returned. Approving a tool once
-- with argument set A therefore also authorized a later call to the same tool
-- with argument set B inside the same run (issue #9).
--
-- The runtime now looks decisions up by the digest of the invocation it is
-- about to run (execution.ApprovalIntentDigest: sha256 over the tool name and
-- the canonical arguments, always 64 hex characters), so the lookup key is
-- (case_id, run_id, tool_name, intent_digest).
--
-- Existing rows keep the empty default and stay fail-closed: the repository
-- refuses an empty lookup key, so a decision persisted before this revision can
-- never be reused by a new call. Both the startup AutoMigrate path and this
-- forward-only script add the same column and index; apply this once, per
-- deployment, when provisioning with Atlas instead of the service.
ALTER TABLE magi_approval_request
    ADD COLUMN intent_digest VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE magi_approval_request
    ADD KEY idx_approval_intent (case_id, run_id, tool_name, intent_digest);
