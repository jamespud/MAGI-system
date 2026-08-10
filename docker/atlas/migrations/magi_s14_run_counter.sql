-- MAGI S14: atomic per-user active-run counter.
-- The counter makes the per-user concurrency limit check-and-increment one
-- statement across replicas (see adapter/run_counter.go). Benchmark runs carry
-- an owner lease (magi_benchmark_run.lease_owner/lease_until) so crashed
-- workers can be resumed by exactly one replica.
-- NOTE: AutoMigrate (backend/adapter/model.go) is the source of truth and adds
-- magi_benchmark_run.lease_owner/lease_until and magi_judge_eval.case_id
-- unique index on startup; the ALTERs are therefore not included here.

CREATE TABLE IF NOT EXISTS magi_user_run_counter (
    user_id BIGINT NOT NULL PRIMARY KEY,
    active_run INT NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
