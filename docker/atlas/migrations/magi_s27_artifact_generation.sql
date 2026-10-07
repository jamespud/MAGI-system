-- MAGI S27: bind authoritative decision artifacts and checkpoints to the
-- durable Case execution generation introduced by S25/T2.
--
-- generation 0 means legacy/unknown provenance. This migration never infers
-- provenance from -aN- identifiers and never backfills legacy rows to a
-- positive generation.
ALTER TABLE magi_agent_run
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_agent_run_case_generation (case_id, execution_generation);

ALTER TABLE evidence_record
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_evidence_case_generation (case_id, execution_generation);

ALTER TABLE claim
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_claim_case_generation (case_id, execution_generation);

ALTER TABLE magi_vote
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_vote_case_generation (case_id, execution_generation);

ALTER TABLE debate_round
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_debate_case_generation (case_id, execution_generation);

ALTER TABLE reflection
    ADD COLUMN case_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_reflection_case_generation (case_id, execution_generation);

-- Legacy reflection/tool-call rows already have a relational AgentRun link.
-- Backfill only Case provenance from that relationship; execution_generation
-- deliberately remains 0 (legacy/unknown). A row whose agent_run_id has no
-- matching magi_agent_run keeps case_id='' instead of failing the upgrade:
-- pre-existing orphan history is preserved as unknown provenance, and is never
-- attributed to a Case by guesswork.
UPDATE reflection AS r
JOIN magi_agent_run AS ar ON ar.id = r.agent_run_id
SET r.case_id = ar.case_id
WHERE r.case_id = '';

ALTER TABLE magi_tool_call
    ADD COLUMN case_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    ADD INDEX idx_tool_call_case_generation (case_id, execution_generation);

UPDATE magi_tool_call AS tc
JOIN magi_agent_run AS ar ON ar.id = tc.agent_run_id
SET tc.case_id = ar.case_id
WHERE tc.case_id = '';

ALTER TABLE resolution
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0;

-- Checkpoints are migrated to the generation contract but deliberately NOT
-- backfilled. The pre-S27 table stores only run_id plus snapshot state: it has
-- no case_id and no agent_run_id, and run_id is the logical working-memory
-- identity ("<case>-<agent>-r<round>-<phase>"), not a declared reference to
-- magi_agent_run.id. Deriving a Case from that string would fabricate
-- provenance, so legacy rows keep case_id='' with execution_generation=0
-- (legacy/unknown).
--
-- CaseRepo.Delete still applies its pre-S27 containment rule (remove rows whose
-- run_id names one of the Case's AgentRun rows) as a deletion rule, not as a
-- provenance claim. A legacy checkpoint that matches neither that rule nor a
-- recorded case_id is retained as unknown-provenance history rather than being
-- attributed to a Case, and the generation-scoped loader cannot return it.
ALTER TABLE magi_agent_checkpoint
    ADD COLUMN case_id VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN execution_generation BIGINT NOT NULL DEFAULT 0,
    DROP PRIMARY KEY,
    ADD PRIMARY KEY (run_id, execution_generation),
    ADD INDEX idx_checkpoint_case_generation (case_id, execution_generation);

-- Rollback warning: once positive-generation artifact/checkpoint rows exist,
-- a writer that ignores execution_generation can mix authority across retries.
-- Downgrade to a generation-unaware writer is unsafe.
