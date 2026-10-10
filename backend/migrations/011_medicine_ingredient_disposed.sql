-- 011: active ingredient and disposal tracking on medicines.
--
-- active_ingredient NULL means a sync client did not send the field; the
-- sync upsert keeps the stored value in that case.
-- disposed_at is set only on soft-deleted records that were thrown away.

ALTER TABLE medicines ADD COLUMN active_ingredient TEXT;
ALTER TABLE medicines ADD COLUMN disposed_at DATETIME;
