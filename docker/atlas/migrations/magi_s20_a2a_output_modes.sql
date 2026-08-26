-- MAGI S20: add additive accepted-output-modes negotiation to A2A submissions.
-- Applied before the new binary. Existing rows default to both advertised
-- modes; an old writer ignores the new column and runs safely during overlap.

ALTER TABLE a2a_submission
  ADD COLUMN accepted_output_modes_json VARCHAR(128) NOT NULL
  DEFAULT '["text/markdown","application/json"]';
