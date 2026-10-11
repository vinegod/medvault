# MedVault

Self-hosted home pharmacy manager. Track every medicine in your house — what's
in stock, what's expiring, who used what, and when. Works on any device on
your home network and offline on your phone.

> Open source. Your data stays on your server. No tracking, no telemetry,
> no third-party cloud.

## What it does

- **Inventory** — name, active ingredient, quantity, unit, category, storage
  location, notes, barcode, and an optional photo per medicine.
- **Filters** — by status (expired, expiring, low stock, opened), category
  and storage location, on top of fuzzy search and sorting.
- **Multiple packs** — packs of the same medicine group into one card with
  the total quantity and the earliest expiry; "Add another pack" copies the
  details.
- **Duplicate-ingredient warning** — the form warns when another medicine
  has the same active ingredient.
- **Two expiration models** — printed expiration date AND post-opening shelf
  life. The card surfaces whichever fires first ("expires in 12 days" can come
  from either).
- **Drug autofill** — start typing a trade name; the form pre-fills the
  active ingredient (INN) and pharma group from a shipped pharmaceutical database
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
- **Disposal and trash** — mark expired medicines as disposed (with
  take-back guidance); restore anything deleted from the Trash page.
- **CSV export and printable checklist** — export the inventory to a
  spreadsheet or print it, optionally for one storage location.
- **Activity log** — append-only per-household audit of who created /
  updated / deleted / disposed what, quantity changes ("10 → 7"), who
  renamed the household, who joined or left. Each medicine shows its own
  history.
- **Account control** — registration can be open, invite-only or closed;
  sign out other devices; delete your account; admin password reset CLI.
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

Requires Docker with the Compose plugin (`docker compose version`).

```bash
git clone https://github.com/vinegod/medvault.git
cd medvault
cp .env.example .env
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -base64 32)|" .env
docker compose up -d --build
```

Check that it is up (`STATUS` turns `healthy` within a few seconds):

```bash
docker compose ps
docker compose logs -f medvault
```

Open `http://your-server-ip:8080` and register. The first account is the
server admin. Once everyone has an account, set `REGISTRATION_MODE=invite`
(or `closed`) in `.env` and run `docker compose up -d` again.

Set `PORT` in `.env` to use another port; set `BIND_ADDR=127.0.0.1` when a
reverse proxy on the same host handles TLS.

The container has `restart: unless-stopped`: it restarts after a crash and
starts again after a reboot, as long as the Docker service starts on boot
(`sudo systemctl enable --now docker` on systemd distros). `docker compose
down` stops it until the next `docker compose up -d`.

Browsers allow the service worker, `crypto.randomUUID`/`crypto.subtle` and
the clipboard only on HTTPS or `localhost`. Over plain HTTP on a LAN address,
offline mode, installing the PWA, adding medicines and photo upload do not
work. Serve MedVault over HTTPS before using it from a phone, then
**Add to Home Screen** to install it. [docs/HTTPS.md](docs/HTTPS.md) covers
Tailscale, Caddy with a public domain, and Caddy on a LAN.

### Development

Backend:

```bash
cd backend
cp ../.env.example .env    # go run reads .env from the current directory
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -base64 32)|" .env
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
| `PORT` | no | `8080` | HTTP listen port. Docker compose also publishes it on the host. |
| `BIND_ADDR` | no | `0.0.0.0` | Docker compose only: host interface the port is published on. |
| `DB_PATH` | no | `./data/medvault.db` | SQLite database file. |
| `MIGRATIONS_DIR` | no | `./migrations` | Numbered SQL migrations applied at startup. |
| `STATIC_DIR` | no | `./static` | Built React assets served on `/*`. |
| `ALLOWED_ORIGINS` | no | `http://localhost:5173` | Comma-separated CORS origins. |
| `TRUSTED_PROXIES` | no | _empty_ | Comma-separated IPs/CIDRs of reverse proxies (e.g. `127.0.0.1`). List only the proxy's own address; a broad range lets other hosts in it forge the client IP. Requests from these peers take the client IP from `X-Forwarded-For`, so per-IP rate limits apply per client. Leave empty when exposed directly. |
| `PHOTO_DIR` | no | `./data/photos` | Where uploaded medicine photos are written. |
| `DRUG_DB_DIR` | no | `./data` | Per-country drug DBs live under `<DIR>/<iso>/drugs.db`. The Docker image sets `/app/drugs`. |
| `DRUG_DB_COUNTRY` | no | `ua` | Active country for sync-time autofill. |
| `BACKUP_DIR` | no | _empty_ | Empty disables scheduled backups. Set to enable. |
| `BACKUP_RETAIN_DAYS` | no | `14` | Pruning window. `0` keeps forever. |
| `BACKUP_HOUR` | no | `3` | Hour of day (local time, `0..23`) to run backup. |
| `TZ` | no | `UTC` | Time zone for `BACKUP_HOUR` and log timestamps, e.g. `Europe/Kyiv`. |
| `REGISTRATION_MODE` | no | `open` | `open`, `invite` (signup needs a household invite code) or `closed` (admin CLI only). The first user can always register. |
| `AI_PROVIDER` | no | `openai` | Server default AI provider: `openai` (any OpenAI-compatible endpoint) or `anthropic`. |
| `AI_BASE_URL` | no | _empty_ | Endpoint base URL, e.g. `http://localhost:11434/v1` for Ollama. Empty means the provider's public API. |
| `AI_API_KEY` | no | _empty_ | API key for the server default model. |
| `AI_MODEL` | no | _empty_ | Default model, used until a server admin sets one in Settings. Empty disables the default. |
| `AI_VISION_MODEL` | no | `AI_MODEL` | Model used for package photos. |
| `AI_TIMEOUT` | no | `60s` | Upper bound for one assistant request. |
| `AI_SECRET` | no | `JWT_SECRET` | Key material for encrypting the API key saved in Settings. Changing it makes the stored key unreadable. |

### Admin CLI

Reset a forgotten password (prints a new random password and signs the
user out everywhere):

```bash
docker compose exec medvault ./medvault reset-password <username>
```

The first registered user is a server admin and can set the AI model in
Settings. Grant or revoke admin rights:

```bash
docker compose exec medvault ./medvault grant-admin <username>
docker compose exec medvault ./medvault revoke-admin <username>
```

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

### Backup and restore (Docker)

All server data (database and photos) lives in the `medvault-data` Docker
volume, mounted at `/app/data`. It survives `docker compose down` and
rebuilds; `docker compose down -v` deletes it.

Daily backups: add to `.env` and run `docker compose up -d`. They run at
`BACKUP_HOUR` (default 3) in the `TZ` time zone:

```bash
BACKUP_DIR=/app/data/backups
TZ=Europe/Kyiv
```

The backups are in the same volume as the database, so copy them off the
machine regularly:

```bash
mkdir -p backups
docker compose cp medvault:/app/data/backups/. ./backups/
```

Restore a backup:

```bash
docker compose stop medvault
gunzip -k backups/medvault-<timestamp>.db.gz
docker compose cp backups/medvault-<timestamp>.db medvault:/app/data/medvault.db
docker compose run --rm --no-deps medvault rm -f /app/data/medvault.db-wal /app/data/medvault.db-shm
docker compose start medvault
```

Backups contain the database only. To keep photos too, copy
`/app/data/photos` the same way. Users can also export their inventory
from Settings (optionally encrypted).

### Upgrading

```bash
git pull
docker compose up -d --build
```

Migrations run automatically at startup. Back up first.

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
- [x] Filters, pack grouping, active ingredient + duplicate warning
- [x] Trash, disposal, per-medicine history
- [x] CSV export + printable inventory
- [x] Registration modes, sign out other devices, account deletion, admin reset
- [ ] Native Android client
- [ ] Soft-delete cleanup job (per-server retention)
- [ ] Per-household autofill country preference

## License

MIT.
