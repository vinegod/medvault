-- 013: server admins and the server-wide AI assistant model.
--
-- The earliest user becomes admin. ai_settings has at most one row;
-- api_key_enc is AES-GCM ciphertext (nonce prepended), NULL means no key.

ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;

UPDATE users SET is_admin = 1
WHERE id = (SELECT id FROM users ORDER BY created_at LIMIT 1);

CREATE TABLE ai_settings (
    id            INTEGER  PRIMARY KEY CHECK (id = 1),
    provider      TEXT     NOT NULL CHECK (provider IN ('openai', 'anthropic')),
    base_url      TEXT     NOT NULL DEFAULT '',
    model         TEXT     NOT NULL,
    vision_model  TEXT     NOT NULL DEFAULT '',
    api_key_enc   BLOB,
    updated_by    TEXT     REFERENCES users(id) ON DELETE SET NULL,
    updated_at    DATETIME NOT NULL
);
