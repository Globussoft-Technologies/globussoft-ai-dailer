-- Add lead_id to call_reviews so post-call reviews can be used as
-- cross-call memory for the voice agent ("Call Memory" proposal,
-- docs/call-memory-proposal.md). The voice prompt builder looks up the
-- last N reviews for a lead at call start via this column.
--
-- Idempotent: safe to re-run. Uses the same add_col_if_missing helper as
-- migrate_app_db_for_go.sql.

DELIMITER $$

CREATE PROCEDURE IF NOT EXISTS add_col_if_missing(
    IN p_table VARCHAR(128),
    IN p_column VARCHAR(128),
    IN p_definition VARCHAR(512)
)
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.COLUMNS
        WHERE TABLE_SCHEMA = DATABASE()
          AND TABLE_NAME = p_table
          AND COLUMN_NAME = p_column
    ) THEN
        SET @ddl = CONCAT('ALTER TABLE ', p_table,
                          ' ADD COLUMN ', p_column, ' ', p_definition);
        PREPARE stmt FROM @ddl;
        EXECUTE stmt;
        DEALLOCATE PREPARE stmt;
    END IF;
END$$

DELIMITER ;

CALL add_col_if_missing('call_reviews', 'lead_id', 'INT DEFAULT NULL');

-- One-time backfill from call_transcripts so historical reviews also
-- become available as call memory. Rows whose transcript was deleted or
-- has no lead stay NULL and are simply never matched.
UPDATE call_reviews cr
  JOIN call_transcripts ct ON cr.transcript_id = ct.id
  SET cr.lead_id = ct.lead_id
  WHERE cr.lead_id IS NULL AND ct.lead_id IS NOT NULL;

-- Index for the per-lead memory lookup at call start.
SET @idx := (
    SELECT COUNT(*) FROM information_schema.STATISTICS
    WHERE TABLE_SCHEMA = DATABASE()
      AND TABLE_NAME = 'call_reviews'
      AND INDEX_NAME = 'idx_call_reviews_lead_id'
);
SET @ddl := IF(@idx = 0,
    'CREATE INDEX idx_call_reviews_lead_id ON call_reviews (lead_id)',
    'SELECT 1');
PREPARE stmt FROM @ddl;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;
