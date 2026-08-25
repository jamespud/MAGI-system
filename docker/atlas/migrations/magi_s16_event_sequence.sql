-- MAGI S16: durable monotonic event sequence per case.
-- Operations: drain old writers between expand/backfill because old writers do
-- not populate seq. After validation, the unique constraint makes the cursor
-- invariant durable for all writers.

-- Expand: keep seq nullable while existing rows and old writers are handled.
ALTER TABLE magi_event
    ADD COLUMN seq BIGINT UNSIGNED NULL;

CREATE TABLE IF NOT EXISTS magi_event_cursor (
    case_id VARCHAR(64) NOT NULL PRIMARY KEY,
    next_seq BIGINT UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Backfill in stable event order before enabling the NOT NULL constraint.
CREATE TEMPORARY TABLE magi_event_seq_backfill AS
SELECT id,
       ROW_NUMBER() OVER (PARTITION BY case_id ORDER BY timestamp, id) AS seq
FROM magi_event;

UPDATE magi_event AS e
JOIN magi_event_seq_backfill AS b ON b.id = e.id
SET e.seq = b.seq;

DROP TEMPORARY TABLE magi_event_seq_backfill;

INSERT INTO magi_event_cursor (case_id, next_seq)
SELECT case_id, MAX(seq) + 1
FROM magi_event
GROUP BY case_id
ON DUPLICATE KEY UPDATE next_seq = GREATEST(next_seq, VALUES(next_seq));

-- Validate before tightening the column and adding the unique key.
DELIMITER $$
CREATE PROCEDURE magi_validate_event_sequence()
BEGIN
    IF EXISTS (SELECT 1 FROM magi_event WHERE seq IS NULL) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'magi_event seq contains NULL';
    END IF;
    IF EXISTS (
        SELECT case_id, seq
        FROM magi_event
        GROUP BY case_id, seq
        HAVING COUNT(*) > 1
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'duplicate magi_event case sequence';
    END IF;
END$$
DELIMITER ;

CALL magi_validate_event_sequence();
DROP PROCEDURE magi_validate_event_sequence;

ALTER TABLE magi_event
    MODIFY COLUMN seq BIGINT UNSIGNED NOT NULL,
    ADD UNIQUE KEY uq_magi_event_case_seq (case_id, seq);
