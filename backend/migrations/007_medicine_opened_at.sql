-- 007_medicine_opened_at.sql
-- Track when liquid/multi-dose medicines were opened. The shelf life of an
-- opened bottle of eye drops or oral suspension is much shorter than the
-- printed expiration date — typically 28 days for drops, 14 for suspensions.
--
-- opened_at is the date the user first opened the package (YYYY-MM-DD,
-- TEXT to avoid the go-sqlite3 DATE auto-parse round-trip issue documented
-- on the expiration_date column in 001_init.sql).
--
-- days_after_opening is the user-overridable shelf life in days. Default
-- comes from a static lookup keyed on `unit` (see backend/internal/database/
-- shelflife.go). Per-medicine value lets the user override when the leaflet
-- says something different.

ALTER TABLE medicines ADD COLUMN opened_at TEXT;
ALTER TABLE medicines ADD COLUMN days_after_opening INTEGER;
