-- 002_households.sql
-- Add multi-user household support.
--
-- Before this migration: medicines.user_id owns a medicine directly.
-- After:  medicines.household_id owns the medicine; the original user_id
--         is kept as created_by for attribution ("who added this?").
--
-- On a fresh install the backfill INSERT/UPDATE statements are no-ops
-- (no existing users or medicines). On an upgrade from 001, they create
-- a personal household for every existing user and reassign their medicines.

CREATE TABLE IF NOT EXISTS households (
    id         TEXT     PRIMARY KEY,
    name       TEXT     NOT NULL,
    created_by TEXT     NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

-- Each row = one user's membership in one household.
-- A user can belong to multiple households (future feature), but in Phase 1
-- the register flow creates exactly one.
CREATE TABLE IF NOT EXISTS memberships (
    user_id      TEXT     NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    household_id TEXT     NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    role         TEXT     NOT NULL DEFAULT 'member'
                          CHECK (role IN ('owner', 'member')),
    joined_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, household_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_household_id ON memberships(household_id);

-- Short-lived invite codes. used_at NULL = not yet redeemed.
CREATE TABLE IF NOT EXISTS household_invites (
    code         TEXT     PRIMARY KEY,
    household_id TEXT     NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    invited_by   TEXT     NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    expires_at   DATETIME NOT NULL,
    used_at      DATETIME,
    used_by      TEXT              REFERENCES users(id)      ON DELETE SET NULL,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_household_invites_household_id ON household_invites(household_id);

-- Add household ownership to medicines.
ALTER TABLE medicines ADD COLUMN household_id TEXT REFERENCES households(id) ON DELETE CASCADE;

-- Rename the original ownership column so its role is clear: it now records
-- who created the entry, not who "owns" the record for access-control purposes.
ALTER TABLE medicines RENAME COLUMN user_id TO created_by;

-- Backfill (no-op on fresh install):
-- 1. Create one personal household per existing user.
INSERT OR IGNORE INTO households (id, name, created_by)
    SELECT id, username || '''s Household', id FROM users;

-- 2. Make each existing user the owner of their personal household.
INSERT OR IGNORE INTO memberships (user_id, household_id, role)
    SELECT id, id, 'owner' FROM users;

-- 3. Assign each medicine to the household of the user who created it.
UPDATE medicines SET household_id = created_by WHERE household_id IS NULL;

-- Swap access-pattern index: queries now scope by household, not by user.
DROP INDEX IF EXISTS idx_medicines_user_id;
CREATE INDEX IF NOT EXISTS idx_medicines_household_id ON medicines(household_id);
