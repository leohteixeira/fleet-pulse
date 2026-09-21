# Fleet Pulse

A single-process fleet simulator. One Go binary hosts an embedded MQTT broker,
in-memory store, HTTP API, and the production UI. Simulated vehicles connect as
real MQTT clients. There is no database and no authentication.

```text
go run ./cmd/server
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
    Store["in-memory store"]
    Ingest["MQTT ingest"]
    Broker["mochi-mqtt v2\n:1883"]
    Sim["simulator\n15–25 MQTT clients"]
    UI["embedded web\ngo:embed"]
  end
  Browser["browser"] -->|GET /api/vehicles\nGET /api/stream\nPOST unlock/lock| HTTP
  Browser -->|static| UI
  HTTP --> Store
  HTTP --> SSE
  HTTP -->|publish command| Broker
  Ingest -->|telemetry + ack| Store
  Store -->|crossing| SSE
  Store --> SSE
  SSE --> Browser
  Sim -->|fleet/vin/telemetry\nfleet/vin/ack| Broker
  Broker --> Ingest
  HTTP -->|fleet/vin/commands| Broker
  Broker --> Sim
```

The HTTP contract is in [`openapi.yaml`](openapi.yaml): snapshot, SSE stream,
unlock, lock, command lookup, and `healthz`.

Simulated vehicles walk a **static OSM extract** of Centro and a west corridor
(`internal/roads/centro.json`), embedded with `go:embed`. The running process
never calls Overpass. Regenerate the file with `go run ./scripts/fetchroads`.

## Trade-offs

The fleet and command machines live in an **in-memory store**. That keeps the
MVP to one process with no Compose or database, and it makes the demo start in
seconds. The cost is total: a restart wipes last-known positions, command
history, and the event feed. Persistence, replicas, and auth are out of scope.

Shutdown drains SSE clients first, then stops the simulator, then closes the
broker.

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
