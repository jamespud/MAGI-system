-- MAGI S19: per-user run admission transaction mutex.
-- This row is only ever locked with SELECT ... FOR UPDATE (MySQL) inside the
-- same transaction that counts queued/running DecisionJob rows. It stores no
-- materialized active count, so a crashed worker never leaks a slot.
--
-- Release boundary: apply S19 first, drain or gate new submissions, replace
-- every replica still using the old RunCounter-based admission, then reopen
-- admission. Do not describe the binary switch as a normal mixed-version
-- rolling deployment: an old replica never acquires this lock, so old and new
-- admission algorithms do not share a mutex during overlap.

CREATE TABLE IF NOT EXISTS magi_user_run_admission_lock (
    user_id BIGINT NOT NULL PRIMARY KEY,
    updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
