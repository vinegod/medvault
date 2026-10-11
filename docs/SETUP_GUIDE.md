# Developer Setup Guide

How to get a working MedVault dev environment up.

## Prerequisites

- **Go** ≥ 1.25 and a C compiler (`gcc`; `go-sqlite3` needs CGO)
- **Node.js** 24 (see `frontend/.nvmrc`)
- **SQLite** CLI (optional — handy for poking at the database)
- **Docker** + **Docker Compose** ≥ 2.24.4 (only needed if you want to run
  the packaged image instead of `go run`)
- **Python** ≥ 3.10 (only needed if you want to regenerate the drug
  autofill database from a CSV)

## Clone

```bash
git clone https://github.com/vinegod/medvault.git
cd medvault
```

## Run the backend

The server reads `.env` from its working directory, so for `go run` the
file is `backend/.env` (Docker uses `.env` in the project root). Create it
and set `JWT_SECRET` to 32+ random bytes:

```bash
cd backend
cp ../.env.example .env
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -base64 32)|" .env
go run .
```

On macOS use `sed -i ''`. The full list of environment variables is in
[README.md](../README.md#configuration).

The server listens on `:8080` (or `PORT` from `.env`). On first boot it:

- Creates `./data/medvault.db` and runs every numbered SQL file under
  `./migrations/` in order.
- Logs the startup banner: `MedVault dev starting on port 8080`
  (release builds print the version instead of `dev`).
- Logs whether any drug autofill DBs were registered (`drug DBs
  registered (lazy-open): [ua]`) or none (`drug DBs: none found under
  ./data (autofill disabled)`).

Health check:

```bash
curl -i http://localhost:8080/api/health
```

You should see `200 OK` and an `X-Request-Id` header.

## Run the frontend

In a second terminal:

```bash
cd frontend
npm install
npm run dev
```

Vite's dev server listens on `http://localhost:5173` and proxies
`/api/*` to the Go backend on `:8080`. Open it in a browser, register
a user, and add a medicine.

`npm run build` writes the production bundle to `backend/static/`, which
`go run .` then serves on `http://localhost:8080`.

## Run the whole thing under Docker

This is what end users run; the README's
[Quick start](../README.md#docker) has the full steps. It uses `.env` in
the project root.

```bash
cp .env.example .env
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -base64 32)|" .env
docker compose up -d --build
docker compose ps        # STATUS: healthy
```

The image is multi-stage:
1. `node` builds the React bundle.
2. `golang` compiles the backend with CGO (needed for `go-sqlite3`).
3. The runtime image is Alpine + the Go binary + `./static/` +
   `./migrations/` + drug DBs from `backend/data/<iso>/` under
   `/app/drugs/`.

`docker-compose.yml` keeps `/app/data` (database, photos) in the named
volume `medvault-data`, so it persists across rebuilds. The container
restarts automatically (`restart: unless-stopped`) and reports health
from `/api/ready`.

## Tests

Backend:

```bash
cd backend
go test ./... -race -count=1
```

Frontend:

```bash
cd frontend
npx vitest run
npx tsc --noEmit
npx eslint src --max-warnings 0
npx prettier --check src
```

i18n parity (every `en.json` key must exist in `uk.json`):

```bash
diff \
  <(jq -r 'paths|join(".")' frontend/src/i18n/locales/en.json | sort) \
  <(jq -r 'paths|join(".")' frontend/src/i18n/locales/uk.json | sort)
```

Empty output = parity.

## Linting

Backend:

```bash
cd backend
gofmt -l .                  # output should be empty
go vet ./...
golangci-lint run           # if you have golangci-lint installed
```

## Drug autofill database (optional)

The repository ships `backend/data/ua/drugs.db` (Ukrainian Державний
реєстр). To regenerate from a fresh CSV:

```bash
cd backend
python3 ../scripts/import_drugs.py --country ua
```

The CSV itself is dev-only (3+ MB, immutable upstream data) and not
committed. Only the compiled `drugs.db` ships.

To add another country, add an adapter to the `SOURCES` registry in
`scripts/import_drugs.py` and drop the resulting `drugs.db` under
`backend/data/<iso>/`. The Go side discovers it on the next boot.

## AI assistant (optional)

Set a default in `.env`, or set the model in Settings as a server admin
(the first registered user). The model needs tool calling; photo scans
need a vision model.

Ollama:

```bash
ollama pull qwen2.5vl:7b
```

```
AI_BASE_URL=http://localhost:11434/v1
AI_MODEL=qwen2.5vl:7b
```

OpenAI:

```
AI_API_KEY=sk-...
AI_MODEL=gpt-4o-mini
```

OpenRouter:

```
AI_BASE_URL=https://openrouter.ai/api/v1
AI_API_KEY=sk-or-...
AI_MODEL=google/gemini-2.5-flash
```

Anthropic:

```
AI_PROVIDER=anthropic
AI_API_KEY=sk-ant-...
AI_MODEL=claude-haiku-5-5
```

From Docker, reach Ollama on the host via `http://host.docker.internal:11434/v1`.

On Linux, add this to the `medvault` service in `docker-compose.yml` for
`host.docker.internal` to resolve:

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

Ollama listens on `127.0.0.1` by default; start it with
`OLLAMA_HOST=0.0.0.0` so the container can reach it.

To debug prompts in the Genkit Dev UI, run the backend with
`GENKIT_ENV=dev` under `genkit start -- go run .`.

## Project layout

```
medvault/
├── backend/                # Go API + static SPA host
│   ├── main.go
│   ├── admin.go            # reset-password, grant-admin, revoke-admin CLI
│   ├── go.mod
│   ├── internal/
│   │   ├── api/            # response envelope helpers
│   │   ├── assistant/      # AI assistant: photo scan, inventory chat (Genkit)
│   │   ├── auth/           # JWT + bcrypt
│   │   ├── database/       # connection, migrations, drug DB registry, backups
│   │   ├── handlers/       # one file per endpoint group
│   │   ├── middleware/     # Recover, RequestID, Logging, RequireAuth, RequireHousehold, RequireAdmin, RateLimiter
│   │   ├── models/         # request/response structs + Validate()
│   │   ├── testutil/       # shared test helpers
│   │   └── version/        # build version
│   ├── migrations/         # numbered SQL files
│   ├── static/             # built frontend (npm run build)
│   └── data/               # runtime — DB + photos + drug DBs (created on first boot)
│
├── frontend/               # React + TypeScript + Vite PWA
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── contexts/
│   │   ├── hooks/
│   │   ├── services/       # api, db (Dexie), sync, photos, notifications, server
│   │   ├── utils/
│   │   ├── i18n/locales/   # en.json, uk.json
│   │   └── sw.ts           # service worker (InjectManifest)
│   └── vite.config.ts
│
├── scripts/
│   └── import_drugs.py     # CSV → drugs.db
│
├── docs/
│   ├── API.md              # full HTTP API reference
│   ├── ARCHITECTURE.md
│   ├── HTTPS.md            # Tailscale and Caddy setups
│   ├── SECURITY.md
│   ├── SYNC.md
│   └── SETUP_GUIDE.md      # this file
│
├── docker-compose.yml
├── docker-compose.caddy.yml # optional HTTPS proxy
├── Caddyfile
├── Dockerfile
├── .env.example
└── README.md
```

## Database layout

SQLite under `./data/medvault.db`. Tables (see `backend/migrations/`):

- `users`, `refresh_tokens`, `user_settings`, `ai_settings`
- `households`, `memberships`, `household_invites`, `household_audit`
- `medicines` (soft-delete via `deleted_at`)

Drug autofill DBs live under `./data/<iso>/drugs.db` and have one table:

- `drugs(id, name, inn, ingredients, pharma_group)` with
  `idx_drugs_name COLLATE NOCASE`.

## Common one-liners

```bash
# Tail the server log with structured JSON pretty-printed
go run . 2>&1 | jq

# Watch the medicines table
sqlite3 backend/data/medvault.db "SELECT id, name, quantity, expiration_date FROM medicines;"

# Reset everything
rm -rf backend/data/medvault.db backend/data/medvault.db-shm backend/data/medvault.db-wal

# Trigger a manual backup (BACKUP_DIR must be set)
# Backups also fire on the BACKUP_HOUR cron — use this just for testing the path.
sqlite3 backend/data/medvault.db "VACUUM INTO 'backup.db'" && gzip backup.db

# Verify EXPLAIN uses the composite index
sqlite3 backend/data/medvault.db \
  "EXPLAIN QUERY PLAN SELECT * FROM medicines WHERE household_id=? ORDER BY updated_at"
```

## Adding a new endpoint (recipe)

1. Add a model in `backend/internal/models/` if it has a request body.
   Implement `Validate()`.
2. Add a handler method in the appropriate `backend/internal/handlers/<group>.go`.
   Read context (UserID / HouseholdID / Role) via the `middleware` helpers.
   Use `api.WriteJSON` / `api.WriteError` / `api.ServerError`.
3. Wire it into `buildRouter` in `backend/main.go`. Pick the right
   middleware stack: rate limiter (`apiLimit` / `syncLimit`) and
   `RequireHousehold` if it's household-scoped.
4. Add a test in the same package using the `newHarness` helper from
   `handlers/sync_validation_test.go` or `handlers/medicines_test.go`.
5. Update `docs/API.md` — that doc is the contract for clients on other
   platforms.
