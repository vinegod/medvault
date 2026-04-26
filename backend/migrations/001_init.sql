-- 001_init.sql
-- Initial database schema for MedVault

PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS medicines (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0,
    unit TEXT NOT NULL DEFAULT 'tablets',
    category TEXT NOT NULL DEFAULT 'general',
    -- Stored as TEXT (YYYY-MM-DD), not DATE: the go-sqlite3 driver auto-parses
    -- DATE/DATETIME columns into time.Time, which round-trips through *string
    -- as RFC3339 ("2026-12-31T00:00:00Z") and breaks the API contract.
    expiration_date TEXT,
    storage_location TEXT,
    barcode TEXT,
    notes TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_medicines_user_id ON medicines(user_id);
CREATE INDEX idx_medicines_updated_at ON medicines(updated_at);
CREATE INDEX idx_medicines_expiration_date ON medicines(expiration_date);
CREATE INDEX idx_medicines_deleted_at ON medicines(deleted_at);
CREATE INDEX idx_medicines_barcode ON medicines(barcode);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_token_hash ON refresh_tokens(token_hash);

CREATE TABLE IF NOT EXISTS user_settings (
    user_id TEXT PRIMARY KEY,
    alert_days TEXT NOT NULL DEFAULT '[30, 60, 90]',
    notification_method TEXT NOT NULL DEFAULT 'browser',
    default_category TEXT NOT NULL DEFAULT 'general',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
