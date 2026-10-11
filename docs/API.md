# MedVault HTTP API

This is a complete reference for the MedVault REST API. It is the contract any
client (web, Android, iOS, CLI, …) implements against.

- **Base URL**: `http://your-server:8080` (no `/v1` — the API is unversioned;
  breaking changes will be announced in CHANGELOG).
- **Encoding**: JSON, UTF-8. All requests with bodies should send
  `Content-Type: application/json` (multipart only on `POST /api/medicines/{id}/photo`).
- **Authentication**: JWT (HS256) Bearer token in the `Authorization` header
  on every endpoint except those marked _public_ below.
- **Time**: All timestamps are RFC 3339 in UTC (`2026-05-06T13:00:00Z`).
  Date-only fields (expiration_date, opened_at) are `YYYY-MM-DD` strings.

## Conventions

### Envelope

Every response uses the same envelope:

```json
{
  "data":  <payload> | null,
  "error": { "code": "...", "message": "..." } | null
}
```

Exactly one of `data` and `error` is non-null per response. The HTTP status
code is the primary signal; the `error.code` enum is for client logic
(e.g. distinguishing UNAUTHORIZED from FORBIDDEN to know whether to refresh).

### Error codes

| Code | HTTP | When |
|------|------|------|
| `VALIDATION_ERROR` | 400 | Body is malformed JSON, a field fails validation, or sync sent too many records. |
| `UNAUTHORIZED` | 401 | Missing, invalid, or expired access token. Client should attempt refresh; on a second 401, prompt re-login. |
| `FORBIDDEN` | 403 | Token is valid but the user has no permission for this resource (no household membership, not the household owner, wrong household in path). |
| `NOT_FOUND` | 404 | Resource does not exist or was soft-deleted. Also returned by drug search when the drug DB or the requested country isn't configured. |
| `CONFLICT` | 409 | Username already taken; already a member of that household. |
| `OWNER_TRANSFER_REQUIRED` | 409 | Sole owner of a household with co-members tried to leave or delete their account. Must `POST .../transfer` first. |
| `REGISTRATION_CLOSED` | 403 | Register while the server runs with `REGISTRATION_MODE=closed`. |
| `INVITE_REQUIRED` | 400 | Register without `invite_code` while the server runs with `REGISTRATION_MODE=invite`. |
| `RATE_LIMITED` | 429 | Rate limit exceeded — see the per-endpoint table below. Back off and retry after the window. |
| `UNAVAILABLE` | 503 | DB ping failed (`/api/ready`); all configured drug DBs failed to open. |
| `UPSTREAM_ERROR` | 502 | The AI provider failed or returned unusable output. Details are only logged. |
| `TIMEOUT` | 504 | The AI provider did not answer within `AI_TIMEOUT`. |
| `INTERNAL` | 500 | Unexpected error. Body is sanitised; the server log has the request id. |

### Request ID

Every response carries an `X-Request-Id` header (UUID). Logs include the
same id. When reporting a bug, copy this header — it lets the operator
grep the server log for the exact request.

### Rate limits

| Scope | Endpoints | Limit |
|-------|-----------|-------|
| Per IP | `POST /api/auth/register` | 5 / min |
| Per IP | `POST /api/auth/login`, `POST /api/auth/logout` | 10 / min |
| Per IP | `POST /api/auth/refresh` | 20 / min |
| Per IP | `GET /api/drugs/search` | 60 / min |
| Per user | `POST /api/sync` | 30 / min, 1 MB body, 1000 records/request |
| Per user | `POST /api/ai/scan`, `POST /api/ai/chat` | 10 / min (on top of the 60 / min below) |
| Per user | All other authenticated endpoints | 60 / min |

### CORS

Origins listed in `ALLOWED_ORIGINS` (comma-separated env var) are allowed
with `Authorization` and `Content-Type` request headers and credentials.
The Vite dev server (`http://localhost:5173`) is the default if unset.

### Security headers

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Strict-Transport-Security: max-age=31536000; includeSubDomains` —
  emitted only when the request arrived over TLS or via a proxy that set
  `X-Forwarded-Proto: https`.

---

## Health & readiness (public)

### `GET /api/health`

Liveness probe — the process is up. Cheap, never touches the DB.

```http
HTTP/1.1 200 OK
{ "data": { "status": "ok" }, "error": null }
```

### `GET /api/ready`

Readiness probe — the process can serve real traffic. Pings the DB.

- `200` `{ "status": "ready" }` when SQLite responds within 2 s.
- `503` `UNAVAILABLE` when the DB is wedged.

---

## Authentication

JWT flow:

1. `POST /auth/register` or `/auth/login` → `(access_token, refresh_token)`.
2. Send `Authorization: Bearer <access_token>` on every authenticated call.
3. When access token expires (default 15 min), call `/auth/refresh` with
   the refresh token. Server rotates: old refresh token is deleted, a new
   pair is returned.
4. On logout, call `/auth/logout` with the refresh token to revoke it.

A `401` from any endpoint is the signal to attempt one refresh; if the
refresh itself returns `401`, the user must log in again.

Refresh tokens are stored hashed server-side. Reusing an already-rotated
token produces `401` and **does not** revoke the new one — it just won't
be accepted.

### `POST /api/auth/register` _(public, 5/min/IP)_

Creates a user in one transaction. Without `invite_code` the user gets a
personal household called `"<username>'s Household"` and owns it. With
`invite_code` the user joins the invited household as a member instead.

Request:

```json
{ "username": "alice", "password": "correct horse battery staple", "invite_code": "ABCD1234" }
```

Validation:
- `username`: 3–30 chars, `[a-zA-Z0-9_]+`.
- `password`: ≥ 8 chars.
- `invite_code`: optional.

`REGISTRATION_MODE` on the server:

| Mode | Behaviour |
|------|-----------|
| `open` (default) | Anyone can register. |
| `invite` | `invite_code` is required. |
| `closed` | Registration is refused; use the admin CLI. |

The first user on an empty server can always register.

Response `201`:

```json
{
  "data": {
    "user": {
      "id": "11111111-2222-3333-4444-555555555555",
      "username": "alice",
      "created_at": "2026-05-06T13:00:00Z"
    },
    "access_token":  "eyJhbGciOi...",
    "refresh_token": "eyJhbGciOi..."
  },
  "error": null
}
```

Errors: `409 CONFLICT` (username taken), `400 VALIDATION_ERROR` (including
an invalid or expired invite code), `400 INVITE_REQUIRED`,
`403 REGISTRATION_CLOSED`.

### `POST /api/auth/login` _(public, 10/min/IP)_

```json
{ "username": "alice", "password": "..." }
```

Response: same shape as register, status `200`.

The "user not found" and "wrong password" paths are timing-equalised
(both run a bcrypt comparison) and return the same error: `401`
`UNAUTHORIZED` with message `"invalid credentials"`. **Do not** rely on
the message text to distinguish them.

### `POST /api/auth/refresh` _(public, 20/min/IP)_

```json
{ "refresh_token": "eyJ..." }
```

Response `200`:

```json
{
  "data": {
    "access_token":  "eyJ...",
    "refresh_token": "eyJ..."
  },
  "error": null
}
```

Notes:
- Rotates: the old refresh token is deleted before the new pair is issued.
- Reusing a rotated token returns `401` `UNAUTHORIZED`.
- Refresh tokens are also rejected after `JWT_REFRESH_TTL` (server clock).

### `POST /api/auth/logout` _(public, 10/min/IP)_

```json
{ "refresh_token": "eyJ..." }
```

Always returns `200`. Best-effort: missing or invalid tokens silently
succeed (don't leak which tokens existed).

### `POST /api/auth/password` _(authenticated, 60/min/user)_

```json
{ "current_password": "...", "new_password": "..." }
```

Validation:
- `new_password` ≥ 8 chars.
- `new_password` must differ from `current_password`.

On success: rotates the user's password hash, **revokes every existing
refresh token** for the user (forcing re-login on every other device),
and returns a fresh pair so the calling device stays logged in.

Response `200`:

```json
{
  "data": { "access_token": "eyJ...", "refresh_token": "eyJ..." },
  "error": null
}
```

### `POST /api/auth/logout-others` _(authenticated, 60/min/user)_

```json
{ "refresh_token": "eyJ..." }
```

Revokes every refresh token of the caller except the one sent. Access
tokens already issued stay valid until they expire (`JWT_ACCESS_TTL`).
Response `200` with `data: null`.

### `DELETE /api/auth/account` _(authenticated, 60/min/user)_

```json
{ "password": "..." }
```

Deletes the caller's account in one transaction:

- Households where the caller is the only member are deleted with their
  medicines.
- In shared households the caller's membership is removed and a
  `household.member.remove` audit row is written. Medicines and households
  the caller created are reassigned to that household's owner.
- Refresh tokens and settings are deleted.

Errors: `401 UNAUTHORIZED` (wrong password), `409 OWNER_TRANSFER_REQUIRED`
(caller is the only owner of a household with other members; the message
lists the household names).

---

## Households

Every authenticated user belongs to at least one household. The user has
exactly one **active** household at a time — that's the scope every
medicines / sync / alerts / audit endpoint operates against.

The "active household" is resolved by middleware:

1. Read `users.active_household_id`.
2. If null, or if the user is no longer a member of that household, fall
   back to any remaining membership and persist it.
3. If the user has no memberships at all → `403 FORBIDDEN`
   (`"no household membership"`).

Three management endpoints intentionally **skip** that middleware so a
freshly-removed user can recover: `GET /api/households`,
`POST /api/households`, `POST /api/households/switch`.

### `GET /api/households`

List every household the caller belongs to.

Response `200`:

```json
{
  "data": [
    {
      "id": "uuid",
      "name": "Smith Family",
      "role": "owner",
      "joined_at": "2026-04-01T08:00:00Z",
      "is_active": true
    },
    {
      "id": "uuid2",
      "name": "Cottage",
      "role": "member",
      "joined_at": "2026-04-10T08:00:00Z",
      "is_active": false
    }
  ],
  "error": null
}
```

### `POST /api/households`

Create a new household, become its owner, and switch to it.

```json
{ "name": "Cottage" }
```

Validation: `name` 1–80 chars (trimmed).

Response `201`: a `Household` (see schema).

### `POST /api/households/switch`

Set the caller's active household.

```json
{ "household_id": "uuid" }
```

Response `200`:

```json
{ "data": { "household_id": "uuid", "role": "owner" }, "error": null }
```

Errors: `403` if the caller isn't a member of that household.

### `GET /api/households/current`

The caller's active household with its members list.

Response `200`:

```json
{
  "data": {
    "id":         "uuid",
    "name":       "Smith Family",
    "created_by": "user-uuid",
    "created_at": "2026-04-01T08:00:00Z",
    "updated_at": "2026-04-01T08:00:00Z",
    "members": [
      {
        "user_id":   "user-uuid",
        "username":  "alice",
        "role":      "owner",
        "joined_at": "2026-04-01T08:00:00Z"
      }
    ]
  },
  "error": null
}
```

### `PATCH /api/households/{id}` _(owner only)_

Rename the household. The path id must equal the caller's active
household id (anti-CSRF / wrong-context guard).

```json
{ "name": "New Name" }
```

Validation: 1–80 chars (trimmed).

Response `200`:

```json
{ "data": { "id": "uuid", "name": "New Name", "updated_at": "..." } }
```

### `DELETE /api/households/{id}/membership`

Caller leaves the household. Behaviour:

- Sole member → the household is **deleted** (cascades to medicines,
  invites, memberships, audit, photos-by-hash stay on disk).
- Sole owner with co-members → `409 OWNER_TRANSFER_REQUIRED`. Caller must
  `POST .../transfer` first.
- Otherwise → membership row deleted; `users.active_household_id`
  cleared if it pointed here.

Response `200` with `null` data on success.

### `POST /api/households/{id}/transfer` _(owner only)_

Promote another member to owner; demote yourself to member.

```json
{ "to_user_id": "uuid" }
```

Validation: `to_user_id` must be present and not the caller.

Response `200` with `null` data.

### `POST /api/households/{id}/invite` _(owner only)_

Generate an 8-character invite code, valid for 7 days. Charset is
unambiguous uppercase alphanumeric (`ABCDEFGHJKLMNPQRSTUVWXYZ23456789` —
no `0/O/1/I`).

Response `201`:

```json
{
  "data": {
    "code":         "ABCD2345",
    "household_id": "uuid",
    "invited_by":   "user-uuid",
    "expires_at":   "2026-05-13T13:00:00Z",
    "used_at":      null,
    "used_by":      null,
    "created_at":   "2026-05-06T13:00:00Z"
  },
  "error": null
}
```

### `GET /api/households/{id}/invites` _(owner only)_

List active (unused, unrevoked, unexpired) invite codes for the
household, newest first.

### `DELETE /api/households/{id}/invites/{code}` _(owner only)_

Revoke an invite. Returns `404 NOT_FOUND` if the code doesn't exist,
was already used, or already revoked. Once revoked, `Join` rejects it
with the same `"invalid or expired"` message it uses for any other
non-redeemable code.

### `POST /api/households/join`

Redeem an invite code. The caller is added as a `member` of the target
household and **switched** to it. Used codes can't be redeemed twice.

```json
{ "code": "ABCD2345" }
```

Validation: `code` non-empty (trimmed).

Errors:
- `400 VALIDATION_ERROR "invalid or expired invite code"` — bad / used / revoked / expired.
- `409 CONFLICT "already a member of this household"`.

Response `200`:

```json
{ "data": { "household_id": "uuid" }, "error": null }
```

### `DELETE /api/households/{id}/members/{user_id}` _(owner only)_

Remove a member. Owners cannot remove themselves (`400 VALIDATION_ERROR`).

Response `200` with `null` data.

### `GET /api/households/{id}/audit?limit=N`

Read the household activity log, newest first. `limit` defaults to 50,
clamped to `[1, 200]`.

Actions logged:
- `medicine.create`, `medicine.update`, `medicine.delete`
- `medicine.quantity` (only the quantity changed), `medicine.dispose`,
  `medicine.restore`
- `household.rename`
- `household.member.add` (any user joins)
- `household.member.remove`
- `household.transfer`

Response `200`:

```json
{
  "data": [
    {
      "id":          "audit-uuid",
      "username":    "alice",
      "action":      "medicine.delete",
      "target_type": "medicine",
      "target_id":   "med-uuid",
      "target_name": "Tylenol",
      "quantity_before": null,
      "quantity_after":  null,
      "at":          "2026-05-06T13:00:00Z"
    }
  ],
  "error": null
}
```

Medicine changes are logged both from the REST endpoints and from
`/api/sync`. `quantity_before` / `quantity_after` are set when a medicine's
quantity changed. A write that changes no visible field, or that loses
last-write-wins, is not logged.

`username` is captured at the moment of the action — it survives the user
being removed from the household later. `target_id` and `target_name` are
nullable (e.g. on a household-level event without a single target).

---

## Medicines

All medicine endpoints are scoped to the caller's active household via the
household middleware. Cross-household ids return `404`, never expose data.

### Schema

```json
{
  "id": "uuid",
  "name": "Ibuprofen 400mg",
  "quantity": 10,
  "unit": "tablets",
  "category": "painkiller",
  "active_ingredient": "ibuprofen",
  "expiration_date": "2027-03-15",
  "opened_at": null,
  "days_after_opening": null,
  "storage_location": "Bathroom cabinet",
  "barcode": "4006750006402",
  "notes": "Take with food",
  "photo_hash": null,
  "created_at": "2026-05-06T13:00:00Z",
  "updated_at": "2026-05-06T13:00:00Z",
  "deleted_at": null,
  "disposed_at": null
}
```

| Field | Type | Validation / behaviour |
|-------|------|------------------------|
| `id` | string (uuid) | Server-assigned on `POST`. Required on `PUT` (path) and on every `sync` change. |
| `name` | string | 1–200 chars after trim, no `<>` (HTML guard). |
| `quantity` | int | 0–99999. |
| `unit` | enum | `tablets`, `capsules`, `ml`, `mg`, `g`, `pieces`, `drops`, `sachets`, `patches`. |
| `category` | string | ≤ 50 chars (trimmed). Free-text; common values: `painkiller`, `vitamin`, `antibiotic`, `general`, … |
| `active_ingredient` | nullable string | ≤ 200 chars, no `<>`. Omitting it (or sending `null`) on `PUT` or in a sync change keeps the stored value. |
| `expiration_date` | nullable string | `YYYY-MM-DD`. Strictly validated. |
| `opened_at` | nullable string | `YYYY-MM-DD`. Date the package was first opened. |
| `days_after_opening` | nullable int | 1–3650. User-overridable post-opening shelf life. |
| `storage_location` | string | ≤ 100 chars. Free-text. |
| `barcode` | string | ≤ 50 chars, `[A-Za-z0-9]*`. |
| `notes` | string | ≤ 500 chars. |
| `photo_hash` | nullable string | SHA-256 hex of the photo blob. Read-only via the medicine endpoints — set it via `POST /api/medicines/{id}/photo`. |
| `created_at` / `updated_at` | RFC3339 | Server-assigned on `POST`; refreshed on `PUT`. |
| `deleted_at` | nullable RFC3339 | Set by `DELETE` (soft delete). Records with `deleted_at` are hidden from `GET` and `alerts` but still flow through `sync`. |
| `disposed_at` | nullable RFC3339 | Set through `sync` together with `deleted_at` when the medicine was thrown away. The server clears it whenever `deleted_at` is null. |

**Effective expiration** (a client concern, not server-computed):
`min(expiration_date, opened_at + days_after_opening)`. The reference
frontend implements this in `utils/effectiveExpiration.ts` and the
service worker mirrors it for background notifications.

### `GET /api/medicines`

List all non-deleted medicines for the active household, ordered by
`name ASC`. Response `200`:

```json
{ "data": [ <Medicine>, ... ], "error": null }
```

This endpoint does not paginate — household-scale data fits comfortably.
Use `POST /api/sync` instead if you want server-side filtering by
`updated_at > last_sync`.

### `POST /api/medicines`

Body: every writable field except `id`, `photo_hash`, and the timestamps.

```json
{
  "name": "Ibuprofen 400mg",
  "quantity": 10,
  "unit": "tablets",
  "category": "painkiller",
  "active_ingredient": "ibuprofen",
  "expiration_date": "2027-03-15",
  "opened_at": null,
  "days_after_opening": null,
  "storage_location": "Bathroom cabinet",
  "barcode": "4006750006402",
  "notes": "Take with food"
}
```

Response `201`: the saved record (with server-assigned id and timestamps).
Writes a `medicine.create` audit row.

### `GET /api/medicines/{id}`

Response `200` with the medicine, or `404 NOT_FOUND` if the id is wrong,
soft-deleted, or in another household.

### `PUT /api/medicines/{id}`

Replace a medicine. Body shape identical to `POST /api/medicines` (all
fields, no `id`). Any household member can update any medicine in their
household. Returns `200` with the updated record. Writes a
`medicine.update` audit row.

### `DELETE /api/medicines/{id}`

Soft-delete: sets `deleted_at`. Response `200` with `null` data.
Subsequent `GET` returns `404`. The record continues to surface in
`POST /api/sync` so deletions propagate to other devices. Writes a
`medicine.delete` audit row.

### `GET /api/medicines/{id}/history?limit=N`

The audit rows for one medicine, newest first, in the same shape as
`GET /api/households/{id}/audit`. `limit` defaults to 50, clamped to
`[1, 200]`. An id outside the caller's household returns an empty list.

---

## Photos

Stored content-addressably under `PHOTO_DIR/<sha256>.jpg`. The medicine
row stores only the hash, so identical images shared across medicines
deduplicate.

### `POST /api/medicines/{id}/photo`

`Content-Type: multipart/form-data` with a single field `photo`.

- 5 MB body cap (`MaxBytesReader`).
- Content-Type must start with `image/` (claimed) **and** the byte sniff
  (`http.DetectContentType`) must agree.
- Replaces any existing photo on the medicine. Old blobs are not deleted
  from disk (other rows may reference the same hash).

Response `200`:

```json
{ "data": { "photo_hash": "<sha256-hex>" }, "error": null }
```

Suggested client behaviour: re-encode to JPEG and resize (e.g. 1024 px
long edge) before upload — saves bandwidth and disk.

### `GET /api/medicines/{id}/photo`

Streams the JPEG bytes:

- `Content-Type: image/jpeg`
- `ETag: "<sha256>"`
- `Cache-Control: private, max-age=31536000, immutable`

Returns `404 NOT_FOUND` if the medicine has no photo, or if the file is
missing on disk (logs a warning). Use `If-None-Match: "<hash>"` to skip
the body when the cached copy is still valid.

### `DELETE /api/medicines/{id}/photo`

Clears `photo_hash` on the medicine row. The blob on disk is left in
place. Response `200` with `null` data.

---

## Sync

### `POST /api/sync`

Bidirectional sync, last-write-wins per record.

Request:

```json
{
  "last_sync": "2026-05-01T08:00:00Z",
  "changes": [ <Medicine>, <Medicine>, ... ]
}
```

- `last_sync`: RFC3339 string, or `null` for the very first sync.
- `changes`: array (max 1000), each is a **full** Medicine record (no
  partial patches). Body capped at 1 MB.

Server steps, atomically per request:

1. For each change in order:
   - Validate fields with the same rules as `POST /api/medicines`
     (skipped when `deleted_at` is set — tombstones stay valid even if
     the record's other fields no longer would).
   - Reject empty `id` → `400 VALIDATION_ERROR`.
   - Clamp `created_at` and `updated_at` if they are more than 5 min in
     the future of server clock (anti-skew).
   - On a brand-new id (no row exists yet) and an empty `category` and/or
     empty `notes`: try to fill them from the configured drug DB by
     case-insensitive exact name match. Failure is silent.
   - Upsert with the LWW guard:
     `WHERE excluded.updated_at > medicines.updated_at AND medicines.household_id = excluded.household_id`.
2. Read all household records with `updated_at > last_sync`
   (or all of them if `last_sync` is `null`). Soft-deleted rows are
   included so deletions propagate.
3. Return them with the current `server_time`.

Response `200`:

```json
{
  "data": {
    "records": [ <Medicine>, ... ],
    "server_time": "2026-05-06T13:00:00Z"
  },
  "error": null
}
```

Errors:
- `400 VALIDATION_ERROR` — body too large, malformed JSON, > 1000 records,
  per-record validation failure (`change <i> (id=<id>): <reason>`).
- `429 RATE_LIMITED` — 30/min per user.

Notes for client implementers:
- A `200` response is atomic in the sense that **all** the changes were
  committed or none of them were (single SQL transaction). Per-record
  validation failure rolls back the whole batch.
- Use `server_time` as your next `last_sync`, not your own clock — that's
  how clock-skew is sidestepped.
- Records with the same id from another device may come back with newer
  `updated_at` than the copy you just sent — apply LWW locally too.

---

## Alerts

### `GET /api/alerts`

Returns expiration buckets for the active household, computed on the
server using the **caller's own** `alert_days` from `user_settings` (the
user viewing the page; see settings below). Falls back to `[30, 60, 90]`
if the user has no settings row.

The smallest entry of `alert_days` becomes the "expiring within N days"
inner bucket; the largest is the outer cutoff.

Response `200`:

```json
{
  "data": {
    "expired":       [ <Medicine>, ... ],
    "expiring_soon": [ <Medicine>, ... ],
    "summary": {
      "expired":                  2,
      "expiring_within_30_days":  3,
      "expiring_within_90_days":  5
    }
  },
  "error": null
}
```

Field names of `summary` are stable identifiers — they map to the *first
two* entries of the user's `alert_days` (sorted ascending). When a user
picks e.g. `[14, 60]`, the `expiring_within_30_days` count is actually
the 14-day count and `expiring_within_90_days` is the 60-day count.
If you need accurate labels, render them client-side from
`user_settings.alert_days`.

The server filter uses `expiration_date` (printed). If you want to
include opened-bottle shelf-life expiration, compute "effective
expiration" per record on the client.

---

## Settings

### `GET /api/settings`

Per-user settings. Returns defaults (`alert_days = [30, 60, 90]`,
`notification_method = "browser"`, `default_category = "general"`)
if the user has no row yet.

```json
{
  "data": {
    "alert_days": [30, 60, 90],
    "notification_method": "browser",
    "default_category": "general"
  },
  "error": null
}
```

### `PUT /api/settings`

Body shape identical to `GET`. Validation:

- `alert_days`: 1–5 entries, each in `1..365`.
- `notification_method`: `"browser"` or `"none"`.
- `default_category`: ≤ 50 chars.

Response `200` with the saved settings.

---

## AI assistant

One model for the whole server, set by a server admin, else taken from
the `AI_*` variables. Scan and chat are household-scoped and never write
data.

### `GET /api/ai/status`

```json
{ "data": { "enabled": true, "source": "web", "local": true }, "error": null }
```

`source` is `web` (saved by an admin), `env` or `none`. `local` is true
when the endpoint is a loopback, private or single-label host.

### `GET /api/ai/settings`

```json
{
  "data": {
    "provider": "openai",
    "base_url": "http://localhost:11434/v1",
    "model": "qwen2.5vl:7b",
    "vision_model": "",
    "api_key_set": false,
    "source": "web",
    "can_edit": true
  },
  "error": null
}
```

The API key is never returned. `base_url` is empty for non-admins.
`can_edit` is true for server admins.

### `PUT /api/ai/settings` _(admin only)_

```json
{
  "provider": "openai",
  "base_url": "http://localhost:11434/v1",
  "model": "qwen2.5vl:7b",
  "vision_model": "",
  "api_key": "sk-..."
}
```

- `provider`: `openai` (any OpenAI-compatible endpoint) or `anthropic`.
- `base_url`: optional http(s) URL without credentials, max 300 chars.
- `model` required, `vision_model` optional (defaults to `model`), max 100 chars each.
- `api_key`: omitted keeps the stored key, `""` clears it. Required for `anthropic`.

A saved config never uses the `.env` API key. Returns the new settings.
`403 FORBIDDEN` for non-admins.

### `DELETE /api/ai/settings` _(admin only)_

Removes the saved settings so the `.env` default applies. Returns the new
settings.

### `POST /api/ai/scan`

`multipart/form-data` with field `photo` (JPEG, PNG or WebP, 5 MB max).

```json
{
  "data": {
    "draft": {
      "name": "Nurofen Forte",
      "quantity": 12,
      "unit": "tablets",
      "category": "",
      "active_ingredient": "ibuprofen",
      "expiration_date": "2027-05-31",
      "opened_at": null,
      "days_after_opening": null,
      "storage_location": "",
      "barcode": "4820001234567",
      "notes": ""
    },
    "matches": [{ "id": "…", "name": "Nurofen", "reason": "barcode" }]
  },
  "error": null
}
```

`draft` has the shape of a `POST /api/medicines` body. Fields that fail
validation are dropped; `name` may be empty. `matches` lists existing
medicines with the same barcode, name or active ingredient.

### `POST /api/ai/chat`

```json
{ "messages": [{ "role": "user", "content": "What do I have for a headache?" }] }
```

1-20 messages, `role` is `user` or `assistant`, content 1-4000 chars,
the last message from the user.

```json
{
  "data": {
    "text": "You have Paracetamol (10 tablets, until 2027-03-01).",
    "warning": "",
    "medicine_ids": ["…"]
  },
  "error": null
}
```

`warning` is set when the symptoms may need a doctor; the answer is still
given. `medicine_ids` are inventory items named in `text`.

Errors: `404 NOT_FOUND` when no model is configured, `409 CONFLICT` when
the stored API key can't be decrypted, `502 UPSTREAM_ERROR`, `504 TIMEOUT`.

## Drug autofill (public, 60/min/IP)

Optional. Enabled when one or more `<DRUG_DB_DIR>/<iso>/drugs.db` files
are present at server startup.

### `GET /api/drugs/search?q=Q[&country=CC][&limit=N]`

- `q` < 2 runes → `200` with `[]` (no DB hit).
- `country` (ISO code) restricts the search to one country DB. Defaults
  to all configured countries merged.
- `limit` clamped to `[1, 50]`, default 10.

Errors:
- `404 NOT_FOUND` `"drug database not configured"` — no drug DB is
  loaded server-side.
- `404 NOT_FOUND` `"country not configured"` — `country=` value isn't a
  loaded country.
- `503 UNAVAILABLE` — every configured DB failed to open at first use.

Response `200`:

```json
{
  "data": [
    {
      "name":         "ABIKLAV",
      "inn":          "amoxicillin",
      "ingredients":  "Amoxicillin; Clavulanic acid",
      "pharma_group": "Antibacterials for systemic use",
      "country":      "ua"
    }
  ],
  "error": null
}
```

Ranking: name-prefix first, then INN/ingredients-prefix, then any
substring; ties broken by `name COLLATE NOCASE`.

---

## Rate limit responses

`429 RATE_LIMITED` example:

```json
{
  "data": null,
  "error": { "code": "RATE_LIMITED", "message": "rate limit exceeded" }
}
```

The server does not currently emit `Retry-After`; the static window is
60 seconds for all buckets. Clients should back off ~1 minute and retry.

---

## Worked examples (curl)

Register and store the tokens:

```bash
BASE=http://localhost:8080
TOKENS=$(curl -s "$BASE/api/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"correcthorsebatterystaple"}' \
  | jq -r '.data | "ACCESS=\(.access_token); REFRESH=\(.refresh_token)"')
eval "$TOKENS"
```

Add a medicine:

```bash
curl -s "$BASE/api/medicines" \
  -H "Authorization: Bearer $ACCESS" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Ibuprofen 400mg",
    "quantity": 12,
    "unit": "tablets",
    "category": "painkiller",
    "expiration_date": "2027-03-15",
    "storage_location": "Bathroom",
    "barcode": "",
    "notes": ""
  }' | jq
```

Sync (full pull on first run):

```bash
curl -s "$BASE/api/sync" \
  -H "Authorization: Bearer $ACCESS" \
  -H 'Content-Type: application/json' \
  -d '{"last_sync": null, "changes": []}' | jq
```

Upload a photo:

```bash
curl -s "$BASE/api/medicines/$ID/photo" \
  -H "Authorization: Bearer $ACCESS" \
  -F 'photo=@./bottle.jpg;type=image/jpeg' | jq
```

Refresh tokens:

```bash
curl -s "$BASE/api/auth/refresh" \
  -H 'Content-Type: application/json' \
  -d "{\"refresh_token\":\"$REFRESH\"}" | jq
```

---

## Implementing a client: the minimum viable flow

1. **Bootstrap.** Show a server-URL field. Hit `GET /api/health`; if
   `200 {"status":"ok"}`, you're talking to a MedVault. Persist the URL.
2. **Auth.** Offer Login + Register. On success, persist the refresh
   token in secure local storage (Android: `EncryptedSharedPreferences`
   or DataStore + Android Keystore). Keep the access token in memory.
3. **Initial sync.** `POST /api/sync` with `{ "last_sync": null,
   "changes": [] }`. Persist all returned records into local storage
   (Room / SQLite). Save `server_time` as `last_sync`.
4. **Local writes.** Mark dirty rows with a `pending` flag; debounce
   ~5 seconds, then push them via `POST /api/sync` with `last_sync`
   set. Apply server response back into local storage with LWW. Flip
   `pending` to `synced` for the rows you actually shipped.
5. **Refresh on 401.** If any authenticated call returns `401`, hit
   `/api/auth/refresh` once. If that also returns `401`, prompt re-login.
6. **Photos.** Upload via `POST .../photo` and cache the bytes locally
   keyed on `photo_hash` so subsequent renders skip the network. Use
   `If-None-Match` to revalidate.
7. **Households.** On startup also `GET /api/households` and
   `GET /api/households/current`. Provide a switcher; switching means
   `POST /api/households/switch`, then a fresh full sync (treat the
   active household as the local DB's "tenant" — wipe and re-fetch).

The reference web client in `frontend/` shows all of this in TypeScript.
The ground truth is this document and the Go handlers under
`backend/internal/handlers/`.
