# Security Design

## Threat model

What we're protecting: a household's medicine inventory — names,
quantities, expiration dates, and (optionally) photos. Low-sensitivity
personal data, not clinical health records.

In scope:
- Opportunistic internet scanners (if the server is exposed to the net).
- Someone on the local network sniffing or replaying.
- A household member accessing data outside their own household.
- Lost or stolen device with the PWA installed.

Out of scope:
- Targeted attacks by motivated adversaries.
- Side-channel attacks against bcrypt / AES-GCM.
- Malicious server operators (you're self-hosting; you're the operator).

## Authentication

JWT (HS256) signed with `JWT_SECRET`.

- **Access token** — 15 min default (`JWT_ACCESS_TTL`). Sent on every
  authenticated request as `Authorization: Bearer <token>`. Carries
  `user_id` and `username` claims.
- **Refresh token** — 48 h default (`JWT_REFRESH_TTL`). Stored hashed
  server-side in `refresh_tokens` table. Rotated on every use: the
  caller-supplied token is deleted before the new pair is returned.

### Password handling

- Hashed with bcrypt at cost factor 12.
- Never logged, never returned in API responses (`json:"-"` on the
  `password_hash` field).
- Validation: minimum 8 characters at the API level. (Frontend offers a
  hint at 8 chars but does not gate submission — the server is the only
  source of truth.)

### Login timing

The "user does not exist" path runs a bcrypt comparison against a dummy
hash so it costs the same wall-clock time as the "user exists, wrong
password" path. Both return the same error: `401 UNAUTHORIZED`,
message `"invalid credentials"`.

### Token storage on clients

The reference web client stores:

- Access token in memory only (lost on page reload, re-fetched via
  refresh).
- Refresh token in the IndexedDB `meta` store. Not in localStorage
  (XSS-vulnerable).
- Server URL in IndexedDB `meta`.

Native clients should use the platform's secure storage equivalent
(Android Keystore via EncryptedSharedPreferences or DataStore;
iOS Keychain).

### Recovery UX

If the browser evicts IndexedDB, two tabs race a refresh, or the rotation
succeeds on the server but the client crashes before persisting the new
token, the user loses their refresh token and the server-side copy is
revoked. There is **no silent recovery** — the user must log in again.
This is the trade-off for refresh-token rotation.

### Password change

`POST /api/auth/password` with current+new password:

- Verifies current password.
- Replaces the bcrypt hash.
- **Revokes every existing refresh token** for this user (forces
  re-login on every other device).
- Issues a fresh pair so the calling device stays logged in.

## Authorization

Three layers stacked in middleware order:

1. **`RequireAuth`** — verifies the JWT signature and expiry, injects
   `user_id` + `username` into the request context.
2. **`RequireHousehold`** — looks up the user's active membership,
   injects `household_id` + `role`. Returns `403 FORBIDDEN
   "no household membership"` if the user has no memberships at all.
3. **Per-handler ownership checks** — owner-only endpoints
   (`POST .../invite`, `PATCH .../{id}`, `POST .../transfer`,
   `DELETE .../members/{user_id}`, `GET .../invites`,
   `DELETE .../invites/{code}`) verify `role == "owner"` and that the
   path id matches the caller's active household id.

Cross-household ids never leak data — every read query is scoped by
`household_id = ?` and returns `404 NOT_FOUND`.

## Input validation

Every mutating endpoint runs `req.Validate()` before touching the DB.
Field rules:

| Field | Rule |
|-------|------|
| `name` | 1–200 chars, no `<>` (HTML guard). |
| `quantity` | 0–99999. |
| `unit` | enum: `tablets`, `capsules`, `ml`, `mg`, `g`, `pieces`, `drops`, `sachets`, `patches`. |
| `category` | ≤ 50 chars (trimmed). |
| `expiration_date` | strict `YYYY-MM-DD`. |
| `opened_at` | strict `YYYY-MM-DD`. |
| `days_after_opening` | 1–3650. |
| `storage_location` | ≤ 100 chars. |
| `barcode` | ≤ 50 chars, `[A-Za-z0-9]*`. |
| `notes` | ≤ 500 chars. |
| `username` | 3–30 chars, `[a-zA-Z0-9_]+`. |
| `password` | ≥ 8 chars. |
| `household.name` | 1–80 chars. |
| `alert_days` | 1–5 entries, each in `1..365`. |

The same rules run on the sync path, so a misbehaving client cannot
slip oversized notes / HTML names / invalid units in via `/api/sync`.
Soft-deleted records are exempt: a tombstone for a record whose other
fields no longer validate is still legitimate.

## Rate limits

| Bucket | Endpoints | Limit |
|--------|-----------|-------|
| per IP | `POST /api/auth/register` | 5 / min |
| per IP | `POST /api/auth/login`, `POST /api/auth/logout` | 10 / min |
| per IP | `POST /api/auth/refresh` | 20 / min |
| per IP | `GET /api/drugs/search` | 60 / min |
| per user | `POST /api/sync` | 30 / min, 1 MB body, 1000 records |
| per user | All other authenticated routes | 60 / min |

Implementation: in-memory token bucket per key (no Redis needed for
single-instance deployment).

## Sync hardening

- 1 MB body cap via `http.MaxBytesReader`.
- 1000 records per request.
- Per-record `Validate()` with a `400` mentioning the index and id.
- Empty record id rejected (would force a server-assigned id the client
  can never reference, leading to duplicates on retry).
- **Clock-skew clamp:** any client `created_at` / `updated_at` more than
  5 minutes ahead of server time is replaced with the server time. A
  wildly wrong device clock cannot win every LWW round forever.

## Photo uploads

- Endpoint: `POST /api/medicines/{id}/photo` (multipart, field `photo`).
- 5 MB body cap.
- Both the claimed `Content-Type` and the byte-sniffed type
  (`http.DetectContentType`) must start with `image/`.
- Stored content-addressably as `<PHOTO_DIR>/<sha256>.jpg`. The DB row
  holds the hex hash. Multiple medicines pointing at the same hash
  dedup automatically.
- Files are written to a temp path in the same directory and renamed,
  so a concurrent reader sees either the old file or the complete new
  one — never a half-written blob.

## CORS

Origins listed in `ALLOWED_ORIGINS` (comma-separated env var) are
allowed with `Authorization` and `Content-Type` request headers and
credentials. The Vite dev server (`http://localhost:5173`) is the
dev default if unset.

In production, lock it to the actual SPA origin (e.g.
`https://medvault.your-domain.com`).

## TLS

Two recommended deployment shapes:

1. **Reverse proxy** (recommended): Caddy or nginx in front, handling
   TLS termination. Set `X-Forwarded-Proto: https` so the server emits
   HSTS. This is what `docker-compose.yml` is sized for.
2. **Built-in TLS**: replace `http.ListenAndServe` with
   `http.ListenAndServeTLS` in `main.go`. Use Let's Encrypt certs.

In either case, the server emits HSTS only on requests it knows arrived
over TLS — never on plain `http://localhost:8080` for dev.

## Encrypted backups (client-side export)

The frontend's "Export" feature offers an optional passphrase. When set:

- AES-GCM with a 256-bit key.
- Key derived via PBKDF2-SHA256, 600,000 iterations, 16-byte random
  salt.
- 12-byte random IV per export.
- Output JSON envelope:

  ```json
  {
    "version":     2,
    "encrypted":   true,
    "iterations":  600000,
    "salt":        "<base64>",
    "iv":          "<base64>",
    "ciphertext":  "<base64>"
  }
  ```

- Plaintext exports use `version: 1` (object form, no `encrypted`
  flag). Import auto-detects which envelope it's reading.
- Wrong passphrase produces an authentication-tag failure — surfaced as
  `"could not decrypt — wrong passphrase?"` rather than a generic
  parse error.

## Server-side scheduled backups

Optional. Enabled when `BACKUP_DIR` is set.

- Daily at `BACKUP_HOUR` (local time, default `03`).
- `VACUUM INTO <tmp>` produces a transactionally consistent snapshot
  (no need to stop the server).
- gzip-compressed → `<BACKUP_DIR>/medvault-<RFC3339>.db.gz`.
- Old files older than `BACKUP_RETAIN_DAYS` (default 14, `0` = forever)
  are pruned after each new write.
- Backups are **not** encrypted at the server. The expectation is that
  the operator already encrypts the backup volume / off-site copy.

## Logging

Structured JSON via `slog`. Per request:

- `method`, `path`, `status`, `duration_ms`, `req_id`, `ip` (host only,
  no port).

Audit-worthy events from handlers (failed login, panic recovered,
photo uploaded, audit-write failure) carry the `req_id` so a single
request can be traced across multiple log lines.

Never logged:
- Passwords (plain or hashed).
- JWT tokens (access or refresh).
- Photo bytes.
- Medicine fields beyond ids — privacy.

## OWASP top 10 quick check

| # | Category | Mitigation |
|---|----------|------------|
| A01 | Broken access control | JWT + RequireHousehold + per-handler role checks; every read scoped by household_id. |
| A02 | Cryptographic failures | bcrypt cost 12; JWT HS256 with strong server secret; AES-GCM + PBKDF2 600k for client backups. |
| A03 | Injection | Parameterised SQL only. Input validation on every field. HTML-tag guard on names. |
| A04 | Insecure design | No admin endpoints. Owner promotion requires explicit transfer. Last-write-wins clamps for clock skew. |
| A05 | Security misconfig | Secure defaults: tight CORS, security headers always on, HSTS when TLS observed. |
| A06 | Vulnerable components | Minimal deps; `go mod tidy` + `npm audit` regularly. |
| A07 | Auth failures | Rate limits on login/register/refresh; refresh-token rotation; timing-equalised user-not-found. |
| A08 | Data integrity | JWT signature verification; clock-skew clamp on sync; full-record replacement (no partial patches). |
| A09 | Logging failures | Structured `slog` with request ids; sensitive fields excluded by design. |
| A10 | SSRF | No outbound HTTP from the server. Drug DB is local SQLite, not a remote API. |
