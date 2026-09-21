# Fleet Pulse

A single-process fleet simulator. One Go binary hosts an embedded MQTT broker,
PostgreSQL-backed store, HTTP API, and the production UI. Simulated vehicles
connect as real MQTT clients. There is no authentication.

`DATABASE_URL` is required. The process exits before listening when it is
missing or invalid. `CALENDAR_RATE` is environment-only (`1h/s`, `4h/s`
default / ×14400, `12h/s`). Invalid or missing values become `4h/s`. There is
no HTTP or UI control that changes the rate. Set `SIM_SEED` to replay the same
first-route assignment and the same first leasing-book profile mix.

## Run with Compose (visitor URL)

Caddy is the public HTTP surface. UI and API share `http://127.0.0.1:3300`.
The app binds `0.0.0.0:8300` only on the compose network. MQTT stays inside
the app container and is **not** published to the host.

```text
cp env.example .env
docker compose up -d --build
```

Then open `http://127.0.0.1:3300` (Frota) and `http://127.0.0.1:3300/carteira`.

| Surface | Host port | Notes |
|---|---:|---|
| Caddy (UI + API) | 3300 | Same origin. Visitor URL. |
| PostgreSQL | 5435 | Optional; for local `go run`. |
| App HTTP | — | Internal `0.0.0.0:8300` only. |
| MQTT broker | — | Internal `1883` only. Not published. |

## Local `go run`

Start only Postgres, then run the binary yourself:

```text
cp env.example .env
docker compose up -d postgres
DATABASE_URL=postgres://fleetpulse:fleetpulse@127.0.0.1:5435/fleetpulse?sslmode=disable go run ./cmd/server
```

Then open `http://127.0.0.1:8300`. Vite on `:3300` is only a development
convenience and conflicts with Caddy if the full stack is already up.

## Architecture

```mermaid
flowchart LR
  subgraph compose["compose project fleet-pulse"]
    Caddy["Caddy :3300"]
    subgraph process["single Go process"]
      HTTP["stdlib HTTP\n:8300"]
      SSE["SSE hub"]
      Store["Postgres store\n+ vehicle_state cache"]
      Ingest["MQTT ingest"]
      Broker["mochi-mqtt v2\ninternal :1883"]
      Sim["simulator\n20 rental + ~50 leasing"]
      UI["embedded web\ngo:embed"]
    end
    PG["PostgreSQL"]
  end
  Browser["browser"] -->|same origin| Caddy
  Caddy -->|/ /carteira /assets| HTTP
  Caddy -->|/api /healthz| HTTP
  HTTP --> Store
  Store --> PG
  HTTP --> SSE
  HTTP -->|publish command| Broker
  Ingest -->|telemetry + ack| Store
  Store --> SSE
  SSE --> Browser
  Sim -->|fleet/vin and leasing/vin| Broker
  Broker --> Ingest
  HTTP -->|fleet/vin/commands| Broker
  Broker --> Sim
```

The HTTP contract is in [`openapi.yaml`](openapi.yaml): snapshot, SSE stream
(`?fleet=rental|leasing`), unlock, lock, command lookup, `GET /api/clock`,
leasing vehicles, contracts, notify, block, cancel, payments, and `healthz`.

Motion follows a **committed Greater São Paulo route library**
(`internal/routes/seed.json`, ~60 POIs and ~300 routes), embedded with
`go:embed`. The running process never calls OSRM, Overpass, or any external
router. Rebuild the seed offline with `go run ./scripts/seedroutes`.
Telemetry timestamps stay real time. `GET /api/clock` reports the simulated
instant, the rate, and wall-clock time. `GET /api/contracts` lists the active
40–60 leasing book. Write routes require `Idempotency-Key`.

Rental vehicles still own the Centro geofence and the west-exit demo VIN.
The Centro OSM extract (`internal/roads/centro.json`) stays for that snap;
regenerate it with `go run ./scripts/fetchroads`. Leasing devices publish as
real MQTT clients on `leasing/{vin}/telemetry|ack` and are never seeded into
`GET /api/vehicles`.

## Trade-offs

There is no login. Every visitor can run every action. Last-known positions
live in PostgreSQL (`vehicles` + `vehicle_state`) with an in-memory
`vehicle_state` cache in front of the upsert. Position history is out of
scope — the process does not sample or store past points. Finished commands
and `audit_log` rows older than 7 days are deleted by a retention job.
`GET /healthz` is ready only when the process is up **and** the pool pings.
Rental door commands stay in process memory; a restart still wipes the 5s
machine and the SSE feed. Auth and replicas remain out of scope.

`DATABASE_URL` is required. Example:

```text
postgres://fleetpulse:fleetpulse@127.0.0.1:5435/fleetpulse?sslmode=disable
```

Compose project name is `fleet-pulse`. Published host ports are Caddy `3300`
and Postgres `5435`. App HTTP `8300` and MQTT `1883` stay on the compose
network.

Logs are `log/slog` JSON. Shutdown drains SSE clients first, then stops the
simulator, then flushes/stops the outbox relay, then closes the broker, then
closes the database pool.

## Develop

```text
pnpm --dir web install --frozen-lockfile
lefthook install
pnpm --dir web test
pnpm --dir web build
cp -r web/dist/. internal/webui/dist/
GOTMPDIR="$PWD/.gotmp" go test ./...
GOTMPDIR="$PWD/.gotmp" go test -race ./...
```

`go:embed` reads `internal/webui/dist`. Rebuild the UI and copy it there before
shipping a binary that serves a new frontend. Unknown non-`/api` paths fall
back to `index.html` so `/`, `/carteira`, and `/carteira/:id` work.

## Continuous integration

GitHub Actions runs on every pull request and on pushes to `main`. The workflow
is [`.github/workflows/ci.yml`](.github/workflows/ci.yml):

1. **Secret scanning** — gitleaks over the full git history.
2. **Web application** — `pnpm --dir web install --frozen-lockfile`, then test
   and build (the build already typechecks).
3. **Go module** — `go mod tidy` must be clean, then `gofmt`, `go vet`,
   `go build`, and `go test -race -shuffle=on`.

The shipping copy of `web/dist` into `internal/webui/dist` is not a CI gate;
Go tests use the already-committed embed artifacts.

The lefthook `pre-push` hook runs the same gate locally (gitleaks, Go, web)
before the push reaches GitHub. Install it with `lefthook install`.
