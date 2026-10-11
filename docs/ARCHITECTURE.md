# Architecture

## What MedVault is, technically

A single Go binary that:

- Serves a JSON REST API at `/api/*`.
- Serves a built React SPA at every other path.
- Stores everything in one local SQLite file (WAL mode).
- Optionally serves shipped drug databases (one SQLite per country) for
  client-side autofill.
- Optionally writes daily backups via `VACUUM INTO` +
  gzip.

```
┌────────────────┐   HTTPS    ┌──────────────────────────────────────┐
│ Web client     │  ───────▶  │ Go server (single binary, port 8080) │
│   (React PWA)  │            │                                      │
│ Android client │            │  /api/*  → handlers + middleware     │
│   (planned)    │            │  /*      → static React SPA          │
└────────────────┘            │                                      │
                              │  data/medvault.db    (SQLite, WAL)   │
                              │  data/photos/<sha>.jpg               │
                              │  data/<iso>/drugs.db (optional)      │
                              │  backups/medvault-*.db.gz (optional) │
                              └──────────────────────────────────────┘
```

There is no separate runtime for the frontend in production — the Go
binary serves the prebuilt React bundle as static files.

## Why these choices

### Go for the backend

- One static binary, no runtime to install.
- Tiny memory footprint — a Raspberry Pi can host it.
- Standard library covers HTTP, JSON, JWT (via `golang-jwt/jwt`), and
  crypto. The only "framework" is `chi` for routing.
- Explicit error handling pushes us toward sound code.

### SQLite for storage

- Zero configuration — one file you can copy.
- WAL mode handles concurrent reads while a writer is in flight.
- Backups are `cp` (or `VACUUM INTO` for a transactionally consistent
  copy).
- Plenty of headroom for household-scale data (dozens of medicines,
  1–5 users, occasional sync).

### React + TypeScript + Vite for the frontend

- Strict-mode TS catches the most common contract bugs.
- Vite gives instant HMR in dev and a tiny rollup-built bundle in prod.
- PWA via Vite's `injectManifest` mode lets us own the service worker
  (`src/sw.ts`).
- IndexedDB via Dexie.js is the local cache; a separate Dexie database
  caches photo blobs by hash.

### Last-write-wins sync

For 1–5 users editing the same household inventory, simultaneous edits
to the same record are vanishingly rare. LWW per record by `updated_at`
is simple, debuggable, and good enough. Details: [SYNC.md](SYNC.md).

### PWA (no native app yet)

- One codebase for Web on every platform.
- "Add to Home Screen" gives an offline-capable shortcut on iOS / Android.
- The native Android client is on the roadmap; the API contract is
  designed to support it (see [API.md](API.md)).

## Request lifecycle

Middleware stack on `/api/*` (in order):

1. **Recover** — turns any panic into a 500 envelope with a logged stack.
2. **RequestID** — UUID per request, propagated via context and surfaced
   in the `X-Request-Id` response header.
3. **Logging** — structured `slog` line per request including
   method, path, status, duration, request id, IP (host without port).
4. **SecurityHeaders** — `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
   `Referrer-Policy`, and HSTS when the request arrived over TLS.
5. **CORS** — allow-listed origins from `ALLOWED_ORIGINS`.

Per-route stacks layered on top:

- Rate limiter (per IP for auth and drug search; per user for everything
  else; stricter limit on `/api/sync`).
- `RequireAuth` — verifies the JWT, injects user_id and username.
- `RequireHousehold` — resolves the active household, falls back to any
  remaining membership when the active pointer is stale, injects
  household_id and role. Skipped on the membership-management routes
  (`GET /api/households`, `POST /api/households`, `POST /api/households/switch`)
  so a user who just left their last household can recover.

## Handler conventions

- Read `userID`, `householdID`, `role` via `middleware.UserIDFrom(ctx)` etc.
- Decode JSON body, run `req.Validate()`, return `400 VALIDATION_ERROR`
  on failure.
- Always parameterise SQL — never string-concatenate user input.
- On internal failures, use `api.ServerError(w, op, err)`. It logs the
  op + error with the request id and writes a sanitised `500 INTERNAL`
  envelope to the client.
- Mutations that members care about (`medicine.{create,update,delete}`,
  `household.{rename,member.add,member.remove,transfer}`) call
  `WriteAudit` so the activity log captures them.

## Data model

```
users (id, username, password_hash, active_household_id, is_admin, ...)
  │
  ├──< memberships (user_id, household_id, role)
  │       │
  │       └──> households (id, name, created_by, ...)
  │              │
  │              ├──< medicines (id, household_id, name, active_ingredient, …,
  │              │               photo_hash, deleted_at, disposed_at)
  │              ├──< household_invites (code, household_id, expires_at, used_at, revoked_at)
  │              └──< household_audit (id, household_id, user_id, action, target_*,
  │                                    quantity_before, quantity_after, at)
  │
  ├──< refresh_tokens (id, user_id, token_hash, expires_at, revoked_at)
  └──< user_settings (user_id, alert_days, notification_method, default_category)

ai_settings (id = 1, provider, base_url, model, vision_model, api_key_enc,
             updated_by, updated_at)   -- server-wide, set by admins
```

- Soft delete: `medicines.deleted_at` for sync propagation;
  `medicines.disposed_at` marks a deleted record that was thrown away.
- Cascades: deleting a household cascades to memberships, medicines,
  invites, and audit. Deleting a user cascades to their refresh tokens,
  user_settings and memberships, and to rows whose `created_by` points at
  them. `DELETE /api/auth/account` therefore reassigns `created_by` on
  medicines and households to the household owner before deleting the
  user.
- `users.active_household_id` uses `ON DELETE SET NULL` so a destroyed
  household leaves the user pointer null rather than orphaned.

## Storage layout (runtime)

```
./data/
├── medvault.db                 # main DB (WAL)
├── medvault.db-shm
├── medvault.db-wal
├── photos/
│   └── <sha256>.jpg            # content-addressable, dedup'd by hash
└── ua/
    └── drugs.db                # optional autofill DB

./backups/
└── medvault-2026-05-06T03-00-00Z.db.gz
```

In the Docker image, `./data` is `/app/data` (the `medvault-data` volume)
and drug DBs are baked into `/app/drugs/<iso>/drugs.db`.

Photos are referenced from the `medicines.photo_hash` column. Multiple
medicines with the same photo (e.g. duplicated rows) share one blob.
Orphaned blobs are not GC'd automatically — a sweep job is on the
roadmap.

## Frontend architecture (brief)

- `services/api.ts` — fetch wrapper. Centralises envelope parsing, single
  in-flight refresh promise, `AbortSignal.timeout(15s)`. Throws typed
  errors (`UnauthorizedError`, `NetworkError`, `ApiError`) the caller
  catches.
- `services/db.ts` — Dexie database `MedVault` with one store `medicines`,
  meta key/value store, audit log.
- `services/sync.ts` — debounced (5 s) push of pending records, periodic
  pull (5 min), online/visibility/manual triggers. Framework-agnostic
  (no React import).
- `services/photos.ts` — separate Dexie database `MedVaultPhotos` for
  blob caching. Resizes uploads to 1024 px JPEG 0.85 before posting.
- `services/notifications.ts` — opt-in PWA periodic background sync
  registration; localStorage-flagged. The service worker (`src/sw.ts`)
  reads IndexedDB directly and computes effective expiration with the
  same logic as the UI.
- `contexts/AuthContext.tsx` — current user, current household, switch
  helpers.

## Operational notes

- All structured logs are JSON via `slog`. Grep by `req_id` to follow
  a single request end-to-end.
- `/api/health` is liveness; `/api/ready` is readiness (does a DB ping).
- Graceful shutdown on SIGINT / SIGTERM with a 10-second drain.
