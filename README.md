# Fleet Pulse

A single-process fleet simulator. One Go binary hosts an embedded MQTT broker,
PostgreSQL-backed store, HTTP API, and the production UI. Simulated vehicles
connect as real MQTT clients. There is no authentication.

PostgreSQL is required. For local `go run`, start it from Compose on host
port `5435` (the workspace matrix leaves that port unassigned):

```text
cp env.example .env
docker compose up -d
```

Then:

```text
DATABASE_URL=postgres://fleetpulse:fleetpulse@127.0.0.1:5435/fleetpulse?sslmode=disable go run ./cmd/server
```

Then open `http://127.0.0.1:8300`. Vite on `:3300` is only a development convenience.

| Surface | Port | Bind |
|---|---:|---|
| HTTP API + embedded UI | 8300 | `0.0.0.0` |
| Vite (optional) | 3300 | `0.0.0.0` |
| MQTT broker | 1883 | `0.0.0.0` |

## Architecture

```mermaid
flowchart LR
  subgraph process["single Go process"]
    HTTP["stdlib HTTP\n:8300"]
    SSE["SSE hub"]
    Store["Postgres store\n+ vehicle_state cache"]
    Ingest["MQTT ingest"]
    Broker["mochi-mqtt v2\n:1883"]
    Sim["simulator\n20 rental + ~50 leasing"]
    UI["embedded web\ngo:embed"]
  end
  PG["PostgreSQL"]
  Browser["browser"] -->|GET /api/vehicles\nGET /api/stream\nPOST unlock/lock| HTTP
  Browser -->|static| UI
  HTTP --> Store
  Store --> PG
  HTTP --> SSE
  HTTP -->|publish command| Broker
  Ingest -->|telemetry + ack| Store
  Store -->|crossing| SSE
  Store --> SSE
  SSE --> Browser
  Sim -->|fleet/vin and leasing/vin| Broker
  Broker --> Ingest
  HTTP -->|fleet/vin/commands| Broker
  Broker --> Sim
```

The HTTP contract is in [`openapi.yaml`](openapi.yaml): snapshot, SSE stream,
unlock, lock, command lookup, and `healthz`.

Motion follows a **committed Greater São Paulo route library**
(`internal/routes/seed.json`, ~60 POIs and ~300 routes), embedded with
`go:embed`. The running process never calls OSRM, Overpass, or any external
router. Rebuild the seed offline with `go run ./scripts/seedroutes`. Set
`SIM_SEED` to replay the same first-route assignment.

Rental vehicles still own the Centro geofence and the west-exit demo VIN.
The Centro OSM extract (`internal/roads/centro.json`) stays for that snap;
regenerate it with `go run ./scripts/fetchroads`. Leasing devices publish as
real MQTT clients on `leasing/{vin}/telemetry|ack` and are never seeded into
`GET /api/vehicles`.

## Trade-offs

Last-known rental positions live in PostgreSQL (`vehicles` + `vehicle_state`)
with an in-memory `vehicle_state` cache in front of the upsert. `GET /healthz`
is ready only when the process is up **and** the pool pings. Rental door
commands stay in process memory; a restart still wipes the 5s machine and the
SSE feed. Auth and replicas remain out of scope.

`DATABASE_URL` is required. The process exits before listening on `:8300` when
it is missing or invalid. Example:

```text
postgres://fleetpulse:fleetpulse@127.0.0.1:5435/fleetpulse?sslmode=disable
```

Compose project name is `fleet-pulse`. It publishes Postgres on `5435` only —
not HTTP `3300`/`8300` or MQTT `1883`.

Shutdown drains SSE clients first, then stops the simulator, then closes the
broker, then closes the database pool.

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
shipping a binary that serves a new frontend.

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
