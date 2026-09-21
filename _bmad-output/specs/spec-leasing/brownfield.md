# Brownfield

Phase 2 extends the shipped Fleet Pulse MVP. Do not regress `../spec-fleet-pulse/`. This file is the survival list an implementer must not invent.

## Stays

- Single Go module, stdlib HTTP, embedded mochi-mqtt v2, real MQTT vehicle clients, slog JSON, `signal.NotifyContext` draining SSE first.
- Ports: HTTP 8300, web 3300, MQTT 1883. Bind `0.0.0.0`.
- Frontend: React 19, TypeScript, Vite, react-leaflet v5, CSS or CSS modules, no state library, no component library, `go:embed`, `index.html` fallback. Phase 2 adds **react-router-dom** as the only new UI library.
- Theme toggle, `fleetpulse-theme`, Esri Canvas World Gray, Portuguese UI copy, English authored docs.
- One `EventSource`, drop oldest, no compression on the stream route. Phase 2 requires `?fleet=`; Frota must call `GET /api/stream?fleet=rental`.
- Rental door machine: `PENDING` → `SENT` → `ACKED` | `FAILED` | `TIMEOUT`, **5 seconds from `SENT`**, one-deep queue, `Idempotency-Key` scoped to vin + action.
- Rental topics `fleet/{vin}/telemetry|commands|ack`.
- Rental Centro geofence, Fora da área, lock, and event feed on the Frota view.
- Consumer-owned store **interface**. Callers do not change because the backing store does.

## Changes

- In-memory store **implementation** is replaced by PostgreSQL (pgx, sqlc, goose) plus an in-memory `vehicle_state` cache. Persistence is no longer an MVP non-goal.
- Simulator is no longer “15–25 random Centro paths from `internal/roads/centro.json` only”. Both fleets consume the versioned São Paulo route library. Rental count becomes about 20; leasing about 50.
- Header gains `Frota` / `Carteira` tabs backed by react-router: `/`, `/carteira`, `/carteira/:id`. Carteira is a second console, not a replacement.
- Public deploy adds Compose PostgreSQL + Caddy. MQTT must not be reachable from the public network. CORS is same-origin. Phase-1 “no required external service” is true only for local `go run` if Postgres is provided by Compose.
- SSE is filtered by `?fleet=`. The server does not fan out the other fleet on that connection.

## Do not

- Reuse rental command states for leasing. Leasing needs `REQUESTED` and `ARMED` and a 30s armed/offline timeout. Do not put a 5s expiry on `ARMED`.
- Put leasing vehicles on `fleet/{vin}/` topics or in `GET /api/vehicles`.
- Drive Carteira off the 360px / 44×44 / 172px-feed geometry. Carteira is 380px / 56×44 / map-above-table, no event feed footer.
- Evaluate block policy in the browser as the source of truth.
- Log raw visitor IPs.
- Call OSRM or any external router at runtime.
