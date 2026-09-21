# Fleet Pulse — agent instructions

Repository-specific extension. Read `/workspace/CLAUDE.md` first; what is written here prevails
only inside `repos/fleet-pulse`.

The product brief is at `docs/fleet-pulse.md`. The visual design to replicate
faithfully is at `docs/design/`.

## State

Greenfield. The repository currently contains a license only. There is no Go module, no
application code, and no frontend.

## Architecture rules

- Single Go module. Packages under `internal/` are organized by responsibility, with short names
  and no cycles. **Never create a `utils` package.**
- Interfaces are small and declared by the **consuming** package, not by the one implementing them.
- HTTP uses the standard library. The MQTT broker is embedded via mochi-mqtt server v2.
- Simulated devices connect as real MQTT clients, not in-process function calls.
- The fleet store is in-memory behind an interface. This is a conscious MVP trade-off and must
  remain documented.
- The simulator instantiates 15–25 vehicles, each in its own goroutine, publishing telemetry
  every 2 seconds.
- Door commands are asynchronous: `POST /api/vehicles/{vin}/unlock` and
  `POST /api/vehicles/{vin}/lock` accept `Idempotency-Key` (scoped to vin + action) and
  respond `202` with a command identifier. Queue is one-deep per vehicle; a third new
  command while the slot is full returns `409`. Expiry is 5 seconds from `SENT`.
- Command states are `PENDING`, `SENT`, `ACKED`, `FAILED`, and `TIMEOUT`.
- API and MQTT identify a vehicle by VIN. The snapshot also carries a short display id
  for the marker chip.
- The SSE hub (`GET /api/stream`) buffers per client and drops oldest events under pressure.
- Structured logs use `log/slog` JSON. Propagate a correlation identifier from HTTP through
  MQTT ack.
- Graceful shutdown with `signal.NotifyContext`, draining streaming clients before exit.
- Frontend: React 19, TypeScript, Vite, react-leaflet v5, plain CSS or CSS modules. No router,
  no state library, no component library. Fleet state in `useReducer` indexed by vehicle id.
  A single EventSource connection, guarded against StrictMode double mount. Static build
  embedded with `go:embed` and an `index.html` fallback. Replicate `docs/design/` faithfully:
  tokens, layout, marker anatomy, command states and degraded-stream treatment. Shipped UI
  copy is Portuguese. Dark default plus light-theme toggle. Esri Canvas World Gray tiles.
- The MVP has no authentication, no database, no replicas, and no end-to-end tests.

### MQTT topics

1. `fleet/{vin}/telemetry` — published by the device.
2. `fleet/{vin}/commands` — published by the server.
3. `fleet/{vin}/ack` — published by the device.

### HTTP API

1. `GET /api/vehicles` — fleet snapshot, allowed polygon, short display id.
2. `GET /api/stream` — SSE event stream (telemetry, command, area-exit); drop oldest.
3. `POST /api/vehicles/{vin}/unlock` — 202 with command id; `Idempotency-Key`.
4. `POST /api/vehicles/{vin}/lock` — same machine, idempotency, and queue as unlock.
5. `GET /api/commands/{id}` — current command state.
6. `GET /healthz` — health check.

Contract documented in `openapi.yaml`.

## Ports

Bind development servers to `0.0.0.0` so port forwarding works.

| Surface | Port |
|---|---:|
| HTTP API | 8300 |
| Web (Vite, or the embedded static app) | 3300 |
| MQTT broker | 1883 |

## Frontend conventions

- React 19 with TypeScript, Vite, react-leaflet v5.
- Plain CSS or CSS modules. No router, no state library, no component library.
- Fleet state lives in `useReducer` indexed by VIN.
- A single EventSource connection, protected against StrictMode double mount.
- Presentation labels (`disponível`, `em uso`, `fora`, `offline`) are derived on the
  frontend from telemetry plus the snapshot polygon.

## Language

Everything in this repository is written in English: code, identifiers, comments, branch names,
commit messages, specifications, READMEs and any other documentation.

## Commands

```bash
go test ./...
go test -race ./...
go run ./cmd/server          # after the Go module exists
# pnpm scripts once the frontend exists
```

Always run Git from this directory, never from `/workspace`. Do not commit automatically.

Commits use `type(scope): description` with a coherent scope
(`backend`, `frontend`, `infra`, etc.), without agent `Co-Authored-By`
trailers. Stage explicit paths.

Before any Go coding, review, debugging, troubleshooting, or setup task,
load the `samber/cc-skills-golang@golang-how-to` skill first — it routes to whichever other Go
skills the task needs.

## Required Go skills

The following Go skills from `samber/cc-skills-golang` MUST always be applied when working on
this project. Load them at the start of every Go-related task, regardless of whether the user
explicitly mentions them.

- `samber/cc-skills-golang@golang-code-style`
- `samber/cc-skills-golang@golang-concurrency`
- `samber/cc-skills-golang@golang-context`
- `samber/cc-skills-golang@golang-data-structures`
- `samber/cc-skills-golang@golang-design-patterns`
- `samber/cc-skills-golang@golang-documentation`
- `samber/cc-skills-golang@golang-error-handling`
- `samber/cc-skills-golang@golang-modernize`
- `samber/cc-skills-golang@golang-naming`
- `samber/cc-skills-golang@golang-safety`
- `samber/cc-skills-golang@golang-security`
- `samber/cc-skills-golang@golang-testing`
- `samber/cc-skills-golang@golang-troubleshooting`
