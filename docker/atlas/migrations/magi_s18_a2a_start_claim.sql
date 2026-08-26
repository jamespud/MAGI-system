-- MAGI S18: additive leased start claims for A2A submission startup.
-- Deploy this migration BEFORE the new binary: both columns are additive and
-- the old writer simply never populates them, so a rolling deployment that
-- briefly runs the old writer remains safe.

ALTER TABLE a2a_submission
  ADD COLUMN start_claim_token VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN start_claim_until DATETIME NULL,
  ADD KEY idx_a2a_submission_start_claim (state, start_claim_until);
