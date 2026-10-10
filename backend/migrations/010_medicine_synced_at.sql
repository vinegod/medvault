-- 010: server-assigned change cursor for /api/sync.
--
-- updated_at is client-supplied (it drives last-write-wins), so filtering
-- sync pulls on it misses offline edits: a change made at 11:00 on an
-- offline phone and uploaded at 13:00 is invisible to a device whose
-- last_sync is 12:00. synced_at is always the server time of the last
-- accepted write and is what the pull query filters on.

ALTER TABLE medicines ADD COLUMN synced_at DATETIME;

UPDATE medicines SET synced_at = updated_at;

DROP INDEX IF EXISTS idx_medicines_household_updated;
CREATE INDEX IF NOT EXISTS idx_medicines_household_synced
    ON medicines(household_id, synced_at);
