# MedVault

Self-hosted home pharmacy manager. Track every medicine in your house — what's
in stock, what's expiring, who used what, and when. Works on any device on
your home network and offline on your phone.

> Open source. Your data stays on your server. No tracking, no telemetry,
> no third-party cloud.

## What it does

- **Inventory** — name, quantity, unit, category, storage location, notes,
  barcode, and an optional photo per medicine.
- **Two expiration models** — printed expiration date AND post-opening shelf
  life. The card surfaces whichever fires first ("expires in 12 days" can come
  from either).
- **Drug autofill** — start typing a trade name; the form pre-fills active
  ingredients, INN, and pharma group from a shipped pharmaceutical database
  (currently Ukrainian Державний реєстр; adapter pattern for adding more).
- **Expiration & low-stock alerts** — separate buckets, configurable
  thresholds (default 30 / 60 / 90 days; default low-stock = 3 units).
- **Optional notifications** — uses the PWA Periodic Background Sync API
  where supported (Chromium); in-app banner fallback elsewhere. No push
  service, no VAPID keys.
- **Restock list** — generates a shareable list of low-stock and out-of-stock
  items with one tap (Web Share API + clipboard fallback).
- **Multi-user households** — invite by 8-character code or QR; transfer
  ownership; leave or remove members; switch between households.
- **Activity log** — append-only per-household audit of who created /
  updated / deleted what, who renamed the household, who joined or left.
- **Sync** — last-write-wins per record across devices and household
  members; full local cache in IndexedDB; works offline.
- **Encrypted backup** — export an AES-GCM-encrypted JSON file with a
  passphrase (PBKDF2-SHA256, 600k iterations); plain JSON also supported.
- **Server-side scheduled backups** — daily VACUUM-into-gzip snapshots with
  configurable retention.
- **i18n** — English and Ukrainian translations shipped.
- **Themes** — light, dark, system; preference stored locally.

## Tech stack

| Layer | Tech |
|-------|------|
| Backend | Go (stdlib + chi router + golang-jwt + go-sqlite3) |
| Database | SQLite (WAL mode, foreign keys) |
| Frontend | React + TypeScript + Vite |
| Offline cache | IndexedDB via Dexie.js |
| PWA | Custom service worker (Vite InjectManifest) |
| Deploy | Docker (single container — Go serves API + static SPA) |

The frontend is served by the same Go binary as the API. There is no
separate Node runtime in production.

## Quick start

### Docker

```bash
git clone https://github.com/vinegod/medvault.git
cd medvault
cp .env.example .env       # set JWT_SECRET (required)
docker compose up --build
```

Open `http://your-server-ip:8080`. On a phone, **Add to Home Screen** to
install the PWA.

### Development

Backend:

```bash
cd backend
cp ../.env.example ../.env
go run .
```

Frontend (Vite dev server, proxies API calls to `:8080`):

```bash
cd frontend
npm install
npm run dev
```

Visit `http://localhost:5173`.

## Configuration

All configuration is via environment variables (or a `.env` file in the
project root).

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | yes | — | HMAC secret for signing JWTs. Use 32+ random bytes. |
| `JWT_ACCESS_TTL` | no | `15m` | Access-token lifetime. Go duration format. |
| `JWT_REFRESH_TTL` | no | `48h` | Refresh-token lifetime. |
| `PORT` | no | `8080` | HTTP listen port. |
| `DB_PATH` | no | `./data/medvault.db` | SQLite database file. |
| `MIGRATIONS_DIR` | no | `./migrations` | Numbered SQL migrations applied at startup. |
| `STATIC_DIR` | no | `./static` | Built React assets served on `/*`. |
| `ALLOWED_ORIGINS` | no | `http://localhost:5173` | Comma-separated CORS origins. |
| `PHOTO_DIR` | no | `./data/photos` | Where uploaded medicine photos are written. |
| `DRUG_DB_DIR` | no | `./data` | Per-country drug DBs live under `<DIR>/<iso>/drugs.db`. |
| `DRUG_DB_COUNTRY` | no | `ua` | Active country for sync-time autofill. |
| `BACKUP_DIR` | no | _empty_ | Empty disables scheduled backups. Set to enable. |
| `BACKUP_RETAIN_DAYS` | no | `14` | Pruning window. `0` keeps forever. |
| `BACKUP_HOUR` | no | `3` | Hour of day (local time, `0..23`) to run backup. |

Static-asset directory layout the binary expects:

```
./static/index.html
./static/assets/...
./migrations/0NN_*.sql
./data/medvault.db          (created on first boot)
./data/photos/<sha256>.jpg  (medicine photos)
./data/<iso>/drugs.db       (optional autofill DBs)
./backups/medvault-*.db.gz  (when BACKUP_DIR is set)
```

## API

The HTTP API is documented in detail in [docs/API.md](docs/API.md), including
request/response shapes, error codes, rate limits, and curl examples.

It's a REST-ish JSON API behind JWT auth. All payloads use the envelope
`{ "data": …, "error": { "code": …, "message": … } | null }`. Soft deletes
and last-write-wins sync make it suitable for offline-first clients on any
platform — see "Building a client" below.

## Building a client (e.g. Android)

The whole product is just an HTTP client over the API. To build a native
client of your own:

1. Read [docs/API.md](docs/API.md) end-to-end. It documents every endpoint,
   the JSON envelope, error codes, validation rules, and rate limits.
2. Read [docs/SYNC.md](docs/SYNC.md) for the sync protocol contract.
3. Read [docs/SECURITY.md](docs/SECURITY.md) for the auth flow (JWT access
   + refresh, bcrypt, rotation rules).
4. The reference web client lives in `frontend/` — a useful sample of the
   call patterns (debounced sync, AbortSignal timeouts, offline queueing).

The API is unversioned. Breaking changes will be announced in CHANGELOG.

## Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full picture.
TL;DR:

```
┌────────────────┐   HTTPS    ┌──────────────────────────────┐
│ Web / Android  │ ────────▶  │ Go server (single binary)    │
│ / iOS client   │            │  ├ /api/*  → REST handlers   │
│                │            │  └ /*      → static SPA      │
└────────────────┘            │            ▼                 │
                              │       SQLite (WAL mode)      │
                              │            ▼                 │
                              │   data/photos/  drugs DBs    │
                              └──────────────────────────────┘
```

## Roadmap

- [x] Go backend + React PWA
- [x] Offline mode (IndexedDB + service worker)
- [x] Multi-user households (invite codes, ownership transfer, QR)
- [x] Sync with last-write-wins conflict resolution
- [x] Light / dark / system theme
- [x] Low-stock alerts + configurable thresholds
- [x] Password change
- [x] Drug autofill (UA Державний реєстр; adapter pattern for more countries)
- [x] Opened-on date + post-opening shelf life
- [x] Optional photos per medicine
- [x] Restock-list export
- [x] Append-only household audit log
- [x] Encrypted JSON export (AES-GCM + PBKDF2)
- [x] Scheduled server-side backups (VACUUM INTO + gzip + retention)
- [x] Periodic background expiration notifications (PWA)
- [ ] Native Android client
- [ ] Soft-delete cleanup job (per-server retention)
- [ ] Per-household autofill country preference

## License

MIT.
