-- 006_drop_unused_indexes.sql
-- Drop indexes that no query in the codebase actually uses:
--   - idx_medicines_updated_at: sync queries always include household_id, so
--     idx_medicines_household_updated (added in 005) covers them.
--   - idx_medicines_deleted_at: every "active medicines" query filters by
--     household_id first, then by deleted_at IS NULL — the composite index
--     plus a small NULL filter is faster than a full scan of this index.
--   - idx_medicines_barcode: no handler queries medicines by barcode.
-- Each unused index slows every INSERT/UPDATE; dropping them is pure win.

DROP INDEX IF EXISTS idx_medicines_updated_at;
DROP INDEX IF EXISTS idx_medicines_deleted_at;
DROP INDEX IF EXISTS idx_medicines_barcode;
