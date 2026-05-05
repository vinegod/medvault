-- 009_household_audit.sql
-- Append-only activity log per household. Records "alice deleted Tylenol"
-- so members of a shared household have provenance for state changes.
--
-- Append-only by convention: no DELETE or UPDATE handlers, just INSERT.
-- Cascade removes the log when the household is deleted.

CREATE TABLE IF NOT EXISTS household_audit (
    id           TEXT     PRIMARY KEY,
    household_id TEXT     NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- user_id is nullable so we can keep audit rows after a member leaves.
    user_id      TEXT              REFERENCES users(id)      ON DELETE SET NULL,
    -- Username at the time of the action — preserved even if the user is
    -- removed later, so historical entries stay readable.
    username     TEXT     NOT NULL,
    action       TEXT     NOT NULL CHECK (action IN (
                              'medicine.create', 'medicine.update', 'medicine.delete',
                              'household.rename',
                              'household.member.add', 'household.member.remove',
                              'household.transfer'
                          )),
    target_type  TEXT     NOT NULL,
    target_id    TEXT,
    target_name  TEXT,
    at           DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_household_audit_household_at
    ON household_audit(household_id, at DESC);
