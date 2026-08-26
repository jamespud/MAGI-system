-- MAGI S16 repair resume.
--
-- Run this ONLY in an offline maintenance window with every event writer
-- drained. The published magi_s16_event_sequence.sql is forward-only and is
-- not edited; if it was interrupted (for example by a rolled-back binary or a
-- mid-script failure), this script resumes the same contract idempotently.
--
-- The script rebuilds magi_event.seq from (timestamp, id) order, so never run
-- it after consumers have resumed against a partially migrated schema.
--
-- It is safe to re-run after interruption at every step. Helper procedures are
-- dropped at the start and the end so an interrupted run can restart.

DELIMITER $$

DROP PROCEDURE IF EXISTS magi_resume_add_seq $$
DROP PROCEDURE IF EXISTS magi_resume_backfill $$
DROP PROCEDURE IF EXISTS magi_resume_set_cursor $$
DROP PROCEDURE IF EXISTS magi_resume_validate $$
DROP PROCEDURE IF EXISTS magi_resume_contract $$
DROP PROCEDURE IF EXISTS magi_pfx_has_column $$

CREATE PROCEDURE magi_pfx_has_column(IN tbl VARCHAR(64), IN col VARCHAR(64), OUT present BOOLEAN)
BEGIN
  DECLARE n INT DEFAULT 0;
  SELECT COUNT(*) INTO n
    FROM INFORMATION_SCHEMA.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = tbl AND COLUMN_NAME = col;
  SET present = (n > 0);
END $$

CREATE PROCEDURE magi_resume_add_seq()
BEGIN
  DECLARE present BOOLEAN;
  CALL magi_pfx_has_column('magi_event', 'seq', present);
  IF NOT present THEN
    ALTER TABLE magi_event ADD COLUMN seq BIGINT UNSIGNED NULL;
  END IF;
END $$

CREATE PROCEDURE magi_resume_backfill()
BEGIN
  DROP TEMPORARY TABLE IF EXISTS magi_event_seq_backfill;
  CREATE TEMPORARY TABLE magi_event_seq_backfill AS
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY case_id ORDER BY timestamp, id) AS seq
      FROM magi_event;
  UPDATE magi_event AS e
    JOIN magi_event_seq_backfill AS b ON b.id = e.id
     SET e.seq = b.seq;
  DROP TEMPORARY TABLE IF EXISTS magi_event_seq_backfill;
END $$

CREATE PROCEDURE magi_resume_set_cursor()
BEGIN
  CREATE TABLE IF NOT EXISTS magi_event_cursor (
    case_id VARCHAR(64) NOT NULL PRIMARY KEY,
    next_seq BIGINT UNSIGNED NOT NULL
  ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

  INSERT INTO magi_event_cursor (case_id, next_seq)
  SELECT case_id, MAX(seq) + 1
    FROM magi_event
   GROUP BY case_id
  ON DUPLICATE KEY UPDATE next_seq = GREATEST(next_seq, VALUES(next_seq));

  -- Ensure every event-bearing case has a cursor; seed 1 for an eventless case
  -- so the verifier never reports a missing cursor.
  INSERT INTO magi_event_cursor (case_id, next_seq)
  SELECT DISTINCT e.case_id, 1
    FROM magi_event e
   WHERE NOT EXISTS (SELECT 1 FROM magi_event_cursor c WHERE c.case_id = e.case_id);
END $$

CREATE PROCEDURE magi_resume_validate()
BEGIN
  IF EXISTS (SELECT 1 FROM magi_event WHERE seq IS NULL) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'magi_event seq contains NULL';
  END IF;
  IF EXISTS (
    SELECT case_id, seq FROM magi_event GROUP BY case_id, seq HAVING COUNT(*) > 1
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'duplicate magi_event case sequence';
  END IF;
END $$

CREATE PROCEDURE magi_resume_contract()
BEGIN
  DECLARE is_nullable VARCHAR(3);
  DECLARE has_exact_unique INT DEFAULT 0;
  DECLARE wrong_name VARCHAR(64) DEFAULT '';

  SELECT IS_NULLABLE INTO is_nullable
    FROM INFORMATION_SCHEMA.COLUMNS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_event' AND COLUMN_NAME = 'seq';
  IF UPPER(is_nullable) = 'YES' THEN
    ALTER TABLE magi_event MODIFY COLUMN seq BIGINT UNSIGNED NOT NULL;
  END IF;

  -- Drop a misleading same-name unique index whose columns are not exactly
  -- (case_id, seq). MySQL cannot DROP INDEX IF EXISTS, so guard by name.
  SET @wrong = '';
  SELECT INDEX_NAME INTO @wrong
    FROM INFORMATION_SCHEMA.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_event'
     AND INDEX_NAME = 'uq_magi_event_case_seq'
     AND NON_UNIQUE = 0
   GROUP BY INDEX_NAME
  HAVING SUM(CASE WHEN COLUMN_NAME = 'case_id' AND SEQ_IN_INDEX = 1 THEN 1 ELSE 0 END) <> 1
      OR SUM(CASE WHEN COLUMN_NAME = 'seq' AND SEQ_IN_INDEX = 2 THEN 1 ELSE 0 END) <> 1
      OR COUNT(*) <> 2
   LIMIT 1;
  IF @wrong <> '' THEN
    SET @drop = CONCAT('ALTER TABLE magi_event DROP INDEX ', @wrong);
    PREPARE s FROM @drop; EXECUTE s; DEALLOCATE PREPARE s;
  END IF;

  SELECT COUNT(*) INTO has_exact_unique
    FROM INFORMATION_SCHEMA.STATISTICS
   WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_event'
     AND NON_UNIQUE = 0
     AND ((COLUMN_NAME = 'case_id' AND SEQ_IN_INDEX = 1) OR (COLUMN_NAME = 'seq' AND SEQ_IN_INDEX = 2))
   GROUP BY INDEX_NAME
  HAVING COUNT(*) = 2
   LIMIT 1;
  IF has_exact_unique = 0 THEN
    ALTER TABLE magi_event ADD UNIQUE KEY uq_magi_event_case_seq (case_id, seq);
  END IF;
END $$

DELIMITER ;

CALL magi_resume_add_seq();
CALL magi_resume_backfill();
CALL magi_resume_set_cursor();
CALL magi_resume_validate();
CALL magi_resume_contract();
CALL magi_resume_validate();

DELIMITER $$
DROP PROCEDURE magi_resume_add_seq $$
DROP PROCEDURE magi_resume_backfill $$
DROP PROCEDURE magi_resume_set_cursor $$
DROP PROCEDURE magi_resume_validate $$
DROP PROCEDURE magi_resume_contract $$
DROP PROCEDURE magi_pfx_has_column $$
DELIMITER ;
