# Developer Setup Guide

How to get a working MedVault dev environment up.

## Prerequisites

- **Go** ≥ 1.22 (for the backend)
- **Node.js** ≥ 20 (for the frontend dev server)
- **SQLite** CLI (optional — handy for poking at the database)
- **Docker** + **docker compose** (only needed if you want to run the
  packaged image instead of `go run`)
- **Python** ≥ 3.10 (only needed if you want to regenerate the drug
  autofill database from a CSV)

## Clone

```bash
git clone https://github.com/vinegod/medvault.git
cd medvault
cp .env.example .env
```

Edit `.env` and set `JWT_SECRET` to 32+ random bytes:

```bash
echo "JWT_SECRET=$(openssl rand -hex 32)" >> .env
```

The full list of environment variables is in [README.md](../README.md#configuration).

## Run the backend

```bash
cd backend
go mod tidy
go run .
```

The server listens on `:8080` (or `PORT` from `.env`). On first boot it:

- Creates `./data/medvault.db` and runs every numbered SQL file under
  `./migrations/` in order.
- Logs the startup banner: `MedVault dev starting on port 8080`.
- Logs whether any drug autofill DBs were registered (`drug DBs:
  registered`) or none (`drug DBs: none found`).

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

## Run the whole thing under Docker

This is what end users run.

```bash
docker compose up --build
```

The image is multi-stage:
1. `node` builds the React bundle into `frontend/dist/`.
2. `golang` compiles the backend with CGO (needed for `go-sqlite3`)
   and copies the built frontend into `./static/`.
3. The runtime image is Alpine + the Go binary + `./static/`.

`docker-compose.yml` mounts `./data` into the container so the
SQLite DB persists across rebuilds.

## Tests

Backend:

```bash
cd backend
go test ./... -race -count=1
```

Frontend:

```bash
cd frontend
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

From Docker, reach Ollama on the host via `http://host.docker.internal:11434/v1`
(add `extra_hosts: ["host.docker.internal:host-gateway"]` on Linux).

To debug prompts in the Genkit Dev UI, run the backend with
`GENKIT_ENV=dev` under `genkit start -- go run .`.

## Project layout

```
medvault/
├── backend/                # Go API + static SPA host
│   ├── main.go
│   ├── go.mod
│   ├── internal/
│   │   ├── api/            # response envelope helpers
│   │   ├── auth/           # JWT + bcrypt
│   │   ├── database/       # connection, migrations, drug DB registry, backups
│   │   ├── handlers/       # one file per endpoint group
│   │   ├── middleware/     # Recover, RequestID, Logging, RequireAuth, RequireHousehold, RateLimiter
│   │   └── models/         # request/response structs + Validate()
│   ├── migrations/         # numbered SQL files
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
│   ├── SECURITY.md
│   ├── SYNC.md
│   └── SETUP_GUIDE.md      # this file
│
├── docker-compose.yml
├── Dockerfile
├── .env.example
└── README.md
```

## Database layout

SQLite under `./data/medvault.db`. Tables (see `backend/migrations/`):

- `users`, `refresh_tokens`, `user_settings`
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
