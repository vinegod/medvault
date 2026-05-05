-- 005_medicine_sync_index.sql
-- Replace the single-column household_id index with a composite
-- (household_id, updated_at) index. Sync queries filter by both columns;
-- the composite lets SQLite jump straight to a household's slice and walk
-- only the rows newer than last_sync. The composite also covers single-
-- column household_id lookups via the leftmost-prefix rule, so dropping
-- the old index is safe.

DROP INDEX IF EXISTS idx_medicines_household_id;
CREATE INDEX IF NOT EXISTS idx_medicines_household_updated
    ON medicines(household_id, updated_at);
