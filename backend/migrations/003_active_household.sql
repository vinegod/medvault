-- 003_active_household.sql
-- Replace the implicit "most recently joined" active-household heuristic
-- with an explicit per-user pointer the user can change via /switch.
--
-- ON DELETE SET NULL is intentional: when a household disappears (sole
-- member leaves, or last household record cascade-deleted), the user's
-- pointer goes null and the middleware picks any remaining membership.

ALTER TABLE users ADD COLUMN active_household_id TEXT
    REFERENCES households(id) ON DELETE SET NULL;

-- Backfill: pick the most-recently-joined household for every existing
-- user. Preserves the prior heuristic so behaviour does not change for
-- users who only have one household (the common case).
UPDATE users SET active_household_id = (
    SELECT household_id FROM memberships
     WHERE memberships.user_id = users.id
     ORDER BY joined_at DESC
     LIMIT 1
);

CREATE INDEX IF NOT EXISTS idx_users_active_household ON users(active_household_id);
