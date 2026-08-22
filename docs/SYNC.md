# Sync Protocol

How the local cache (Web: IndexedDB; Android: Room/SQLite) stays
consistent with the server and across other devices in the same household.

## Source of truth

- **Server** is the household-wide aggregator: what other devices sync
  against, and what survives if any single device is wiped.
- **Local cache** is the authoritative copy *for the device*. UI reads
  always go to local storage so the app works offline.
- **Conflicts** are resolved per record by `updated_at` last-write-wins.
  Whichever side has the newer timestamp wins; the other side is
  silently overwritten.

## Payload contract

The client always sends **full** Medicine objects in `changes[]` —
never partial patches. This eliminates ambiguity about whether omitted
fields should be cleared or preserved. The server overwrites its copy
in full when the client `updated_at` is newer.

## Endpoint

`POST /api/sync` — see [API.md](API.md#sync) for the request/response
schema, error codes, and rate limits. This document covers the
client-side state machine.

## Modes of operation

### Server reachable

- Sync runs in the background after local writes (debounced 5 s).
- Sync runs on app open, when coming back online, and every 5 minutes.
- Local writes are always instant — sync is async and non-blocking.

### Server configured but unreachable

- App works exactly like offline mode.
- Pending changes accumulate locally with `sync_status: "pending"`.
- When the server becomes reachable, all pending changes go up at once.

## Client state machine

### Initial sync (first connection to a server)

1. User enters server URL and authenticates.
2. Client calls `POST /api/sync` with `{ "last_sync": null, "changes": [] }`
   (or with all current local records if migrating from offline-only mode).
3. Server returns every record in the household plus `server_time`.
4. Client merges server records into local storage, marks them
   `sync_status: "synced"`.
5. Client persists `server_time` as `last_sync`.

### Regular sync

1. Client gathers records where `sync_status = "pending"` from local
   storage.
2. Snapshot the ids being sent (don't flip status on records the user
   modified again *during* the request).
3. POST `{ last_sync, changes: [<full Medicine>, ...] }`.
4. Server applies the LWW upsert per record (atomic per request).
5. Server queries every record with `updated_at > last_sync` and
   returns it plus current `server_time`.
6. Client merges server records into local storage:
   - For each server record, compare `updated_at` with the local copy.
   - If server is newer → overwrite local.
   - Otherwise → keep local (it was already in flight).
7. Client flips `sync_status` to `"synced"` only on records present in
   the snapshot from step 2.
8. Client persists the new `server_time` as `last_sync`.

### Triggers

Sync runs when:

1. The browser fires `online` (after a flap).
2. The app is foregrounded (visibility change).
3. After any local write (debounced 5 seconds).
4. On a 5-minute timer while the app is open and online.
5. Manual: the user taps "Sync Now" in Settings.

## Conflict resolution

**Last-write-wins per record by `updated_at`.**

- Same record edited on two devices → the later timestamp wins.
- The losing version is silently overwritten.
- No user intervention.

This works for MedVault because:

- 1–5 users in a household.
- Edits are typically "I took 2 pills" (quantity change) from one
  device.
- Simultaneous edits to the same record are vanishingly rare.
- Even if it happens, losing a quantity change is low-stakes — recount
  the bottle.

If you ever need stronger conflict handling, the path is:

1. Add a `version` integer per record.
2. Have the server reject upserts with stale `version` and surface a
   `409 CONFLICT`.
3. Show conflicts to the user for manual resolution.

That isn't on the roadmap; LWW is sufficient for the use case.

## Soft deletes

Records are never physically deleted — they get a `deleted_at`
timestamp.

This is critical for sync:

- A hard delete couldn't propagate to other devices (the row is gone,
  there's nothing to compare timestamps against).
- A tombstone with `deleted_at` set syncs normally; receiving devices
  remove it from their UI.

Per-record validation is **skipped** when `deleted_at` is set. A
tombstone whose original fields no longer validate (e.g. notes once
exceeded the limit) is still legitimate.

The reference web client filters `deleted_at IS NULL` on every UI
query so soft-deleted rows never render.

## Clock-skew handling

The server clamps any client-supplied `created_at` / `updated_at` more
than 5 minutes in the future to the server time. This stops a device
with a wildly wrong clock from winning every LWW round forever.

Clients should use `server_time` from the response — not their own
clock — as the next `last_sync`.

## Photos

Photos do **not** travel through `/api/sync`. They have their own
out-of-band endpoints:

- `POST /api/medicines/{id}/photo` — multipart upload.
- `GET  /api/medicines/{id}/photo` — JPEG bytes with strong ETag.
- `DELETE /api/medicines/{id}/photo` — clears `photo_hash` on the row.

Sync carries only `photo_hash`. After receiving an updated record with
a new hash, fetch the bytes lazily on first display and cache them
locally keyed by hash (the web client stores them in a separate Dexie
DB; an Android client should use a file-cache keyed by hash).

`If-None-Match: "<hash>"` against the photo endpoint short-circuits to
`304 Not Modified` if your cached copy is current.

## Drug autofill enrichment (server side)

When `POST /api/sync` sees a brand-new record id whose `category`
and/or `notes` is empty, the server attempts an exact case-insensitive
name lookup against the configured drug DB. If a match is found:

- Empty `category` is filled with `ingredients`.
- Empty `notes` is filled with `joinDrugNotes(inn, pharma_group)`.

The server only does this on first insert (subsequent edits skip the
lookup), so user edits never get overwritten. Failure of the lookup
is silent and does not block the sync.

## Export / import

The frontend offers JSON export/import for offline backups. Two
envelopes are supported:

- **Plaintext (v1):**
  ```json
  { "version": 1, "exported_at": "...", "medicines": [ ... ], "settings": [ ... ] }
  ```
- **Encrypted (v2):** AES-GCM + PBKDF2-SHA256 (600k iterations).
  See [SECURITY.md](SECURITY.md#encrypted-backups-client-side-export)
  for the envelope shape.

Import detects which form it's reading and prompts for a passphrase
when needed. Records merge LWW into local storage; imported rows
are flagged `pending` so they go up on the next sync if a server is
configured.

## Local schema (web client, reference)

Dexie-managed IndexedDB:

```ts
const db = new Dexie("MedVault");
db.version(N).stores({
  medicines: "id, name, category, expiration_date, updated_at, sync_status, deleted_at",
  meta:      "key",   // last_sync, server_url, refresh_token, …
  audit:     "id, at",
});
```

`sync_status` is **client-only** — it is never sent to the server
(the server doesn't store it). Values:

- `"local"` — never synced; no server configured.
- `"pending"` — modified locally, not yet pushed.
- `"synced"` — matches the server.

Photos live in a separate Dexie database (`MedVaultPhotos`) keyed by
`photo_hash`. This separation is so wiping one database (e.g. on
"Disconnect Server") doesn't blow away cached photos.

## Disconnecting from a server

The reference flow when the user disconnects:

1. Auth tokens removed from `meta`.
2. Server URL cleared from `meta`.
3. All medicine rows updated to `sync_status: "local"`.
4. Sync module goes dormant.
5. App continues working with all data intact in local storage.

## Soft-delete cleanup (TODO, not implemented)

Soft-deleted rows accumulate indefinitely. Plan:

- A periodic job hard-deletes rows where `deleted_at` is older than a
  retention window (proposed default: 90 days).
- The window becomes a per-server setting.
- All actively syncing clients will have absorbed the tombstone well
  inside that window.

Until this lands, the impact is small: a few hundred bytes per deleted
medicine forever. Manual cleanup with a SQL `DELETE FROM medicines
WHERE deleted_at < datetime('now', '-90 days')` is safe.
