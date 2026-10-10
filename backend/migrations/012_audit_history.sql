-- 012: per-medicine history.
--
-- Adds quantity_before / quantity_after and the restore, dispose and
-- quantity actions. The table is rebuilt to replace the CHECK constraint.

CREATE TABLE household_audit_new (
    id              TEXT     PRIMARY KEY,
    household_id    TEXT     NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id         TEXT              REFERENCES users(id)      ON DELETE SET NULL,
    username        TEXT     NOT NULL,
    action          TEXT     NOT NULL CHECK (action IN (
                                 'medicine.create', 'medicine.update', 'medicine.delete',
                                 'medicine.restore', 'medicine.dispose', 'medicine.quantity',
                                 'household.rename',
                                 'household.member.add', 'household.member.remove',
                                 'household.transfer'
                             )),
    target_type     TEXT     NOT NULL,
    target_id       TEXT,
    target_name     TEXT,
    quantity_before INTEGER,
    quantity_after  INTEGER,
    at              DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO household_audit_new
    (id, household_id, user_id, username, action, target_type, target_id, target_name, at)
SELECT id, household_id, user_id, username, action, target_type, target_id, target_name, at
  FROM household_audit;

DROP TABLE household_audit;

ALTER TABLE household_audit_new RENAME TO household_audit;

CREATE INDEX IF NOT EXISTS idx_household_audit_household_at
    ON household_audit(household_id, at DESC);

CREATE INDEX IF NOT EXISTS idx_household_audit_target
    ON household_audit(household_id, target_id, at DESC);
