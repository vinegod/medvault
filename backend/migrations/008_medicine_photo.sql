-- 008_medicine_photo.sql
-- Add an optional photo to medicine records. The photo bytes live on disk
-- under DRUG_PHOTO_DIR/<sha256>.jpg; the row stores only the hash so the
-- DB doesn't bloat with binary data and so identical photos shared across
-- medicines deduplicate automatically.
--
-- Photos are uploaded via a separate POST /api/medicines/{id}/photo endpoint
-- (multipart) — they intentionally do not travel through the 1 MB sync envelope.

ALTER TABLE medicines ADD COLUMN photo_hash TEXT;
