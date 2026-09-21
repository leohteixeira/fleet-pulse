# Architecture

Runtime topology, persistence, simulation, HTTP, and deploy. Diagrams: `architecture-diagrams.md`. Block machine: `state-machines.md`. Phase-1 survival: `brownfield.md`. Visual contract: `ux.md`.

## Process

One Go process, one module. PostgreSQL is the first required external service.

| Surface | Owner | Port | Bind |
|---|---|---:|---|
| HTTP API + embedded UI | `cmd/server` | 8300 (API), 3300 (Vite / embedded web) | `0.0.0.0` |
| MQTT broker | embedded mochi-mqtt server v2 | 1883 | internal network only in public deploy |
| PostgreSQL | Compose service |  — (not published on the public VPS) | compose network |

Startup: migrations, broker, simulator, ingest/store, outbox relay, HTTP. Simulated vehicles still connect as **real MQTT clients**.

Layout stays:

```text
cmd/server/
internal/          # short names, by responsibility, no cycles, no utils
web/               # Vite app; production build embedded with go:embed
migrations/
openapi.yaml
```

Interfaces are small and declared by the **consuming** package. The phase-1 store interface stays; its in-memory implementation is replaced.

## Persistence

PostgreSQL, driver **pgx**, queries **sqlc**, migrations **goose**. Run migrations on process start.

| Table | Role |
|---|---|
| `vehicles` | Both fleets. Fleet discriminator is required so queries never mix. |
| `vehicle_state` | Last-known telemetry. Upsert. Cached in memory in front of the row. |
| `routes` | Versioned seed library. Read-only at runtime. |
| `customers` | Leasing clients and their routine (home, work, weekend). |
| `contracts` | Client, vehicle, installment value, totals, paid count, payer profile, dates. |
| `installments` | Per-due-date rows; status derived against the simulated clock. |
| `payments` | Recorded payments (visitor or system). |
| `commands` | Block and unlock records and their machine. |
| `audit_log` | Append-only. |
| `outbox` | MQTT payloads committed with the domain write. |

Transactional outbox: command row, audit row, and outbox row in **one transaction**. A relay publishes to MQTT and marks sent. Unlock-pending for an offline device is a persisted command, not a memory flag.

Optional, first cut: leasing position history, sampled once per minute, retained 7 days by a cleanup job. Cleanup also bounds finished commands and audit so disk stays stable across days.

## Fleets and MQTT

Fleets never share screens, logical topics, or queries.

| Topic | Publisher | Fleet |
|---|---|---|
| `fleet/{vin}/telemetry` | rental device | rental |
| `fleet/{vin}/commands` | server | rental door lock/unlock |
| `fleet/{vin}/ack` | rental device | rental |
| `leasing/{vin}/telemetry` | leasing device | leasing |
| `leasing/{vin}/commands` | server (via outbox) | block / unlock |
| `leasing/{vin}/ack` | leasing device | leasing |

Rental door commands keep the phase-1 5s machine. Leasing block commands use the machine in `state-machines.md`.

## Simulator

Offline script builds a versioned seed: about 60 São Paulo points of interest, about 300 real routes via OSRM, **once**. Production never calls an external router.

| Fleet | Count | Motion |
|---|---|---|
| Rental | about 20 | Random trips between seed points, variable dwell. |
| Leasing | about 50 | Per-client routine (home, work, weekend events) with jittered clocks. |

Shared: noise on speed, publish interval, and battery; incident scheduler (temporary offline, low battery, signal loss); configurable RNG seed so a scenario can be replayed in development. Reachable leasing devices refuse about **15%** of block executions.

A blocked leasing vehicle does not ignite or move. An unlock-pending vehicle stays still until the pending unlock is acked.

## Simulated calendar and book

Default advance is **4 simulated hours per real second** (`×14400`). Supported rates: `1h/s`, `4h/s`, `12h/s`, set by environment only. Telemetry timestamps stay real time. `GET /api/clock` exposes the simulated date and the current rate. The UI shows the `×N` badge and does not expose a control.

Payer profiles, assigned at contract creation: **40%** pontual, **30%** atrasa ocasionalmente, **20%** regulariza após notificação, **10%** inadimplente. The system generates payments from the profile at each due date, including after visitor intervention (self-heal).

Lifecycle: contracts end, new contracts are created, active book stays **40–60**. Self-heal unblocks any vehicle blocked longer than **30 simulated days** and keeps at least **10%** of active contracts in each overdue band.

## HTTP

Document in `openapi.yaml`. Frontend talks only through this contract. Policy, interval, rate limit, command expiry, and delinquency stay on the server.

| Method | Path | Contract |
|---|---|---|
| `GET` | `/api/clock` | Simulated date, rate, real time. |
| `GET` | `/api/vehicles` | Rental snapshot (phase 1). |
| `GET` | `/api/leasing/vehicles` | Leasing snapshot for first paint. |
| `GET` | `/api/contracts` | Book list. Filters: overdue band, vehicle state. |
| `GET` | `/api/contracts/{id}` | Contract plus installments and audit. |
| `POST` | `/api/contracts/{id}/notify` | `Idempotency-Key`. Notify. |
| `POST` | `/api/contracts/{id}/block` | `Idempotency-Key`, mandatory reason. `202` + command id. |
| `POST` | `/api/contracts/{id}/block/cancel` | `Idempotency-Key`. Cancel armed / release blocked. |
| `POST` | `/api/contracts/{id}/payments` | `Idempotency-Key`. Manual payment. |
| `GET` | `/api/stream?fleet=rental\|leasing` | SSE. Query **required**. Server pushes only that fleet. Missing or invalid `fleet` → `400`. Per-client buffer; drop **oldest**. No compression. Cap 64 connections. Switching `fleet` is a new EventSource; the UI must not treat that swap as `STREAM CAIU`. |
| `POST` | `/api/vehicles/{vin}/unlock` | Unchanged phase-1 door command. |
| `POST` | `/api/vehicles/{vin}/lock` | Unchanged phase-1 door command. |
| `GET` | `/api/commands/{id}` | Command state (rental or leasing). |
| `GET` | `/healthz` | Liveness of app (and readiness that the database is reachable). |

Write statuses the UI must treat:

| Status | When |
|---|---|
| `202` | Block accepted. |
| `409` | Minimum 20s interval on the same contract, or other conflict. Readable error code. |
| `422` | Policy or validation (late, notify, notify age, device offline, reason too short). Readable error code. |
| `429` | 6 writes / 60s / IP. `Retry-After` required. |

## Public-demo protection

- No login. Every visitor can run every action.
- Rate limit by IP on write routes: `429` + `Retry-After`.
- Minimum interval per contract, visitor-independent: `409`.
- `Idempotency-Key` required on writes.
- Self-heal: payer profiles, **30 simulated-day** max block, **10%** floor per overdue band.
- Cap **64** simultaneous SSE connections.
- MQTT internal-only.
- Security headers. CORS **same-origin only** (Caddy serves UI and API on one host).
- Validated payloads: JSON body max **8 KiB**; reason **8–280** characters.

## Frontend build

React 19, TypeScript, Vite, react-leaflet v5, CSS or CSS modules, **react-router-dom**. Routes: `/` Frota, `/carteira` Carteira, `/carteira/:id` selected contract. No state library, no component library. One `EventSource` at a time (`?fleet=` matches the route), guarded against React 19 StrictMode double mount. Production build is `go:embed`'d; unknown non-API paths fall back to `index.html`.

Mock logic inside `<script type="text/x-dc">` does **not** ship. Guards, calendar, movement, and command outcomes are server-owned; the browser renders, filters, selects, and posts.

Theme, tiles, and `localStorage` key `fleetpulse-theme` are unchanged from phase 1.

## Logs and shutdown

`log/slog` JSON. One correlation id from the HTTP write through outbox publish to the MQTT ack. Never log tokens, raw IPs, or raw `Idempotency-Key` values as secrets; the audit stores the hashed visitor id, not the IP.

Shutdown: `signal.NotifyContext`. Drain SSE clients first, stop the simulator, flush/stop the outbox relay, close the broker, close the database pool, exit.

## Packaging and deploy

One binary with the frontend embedded. Compose project: app, PostgreSQL, Caddy with automatic HTTPS. Healthchecks on app and database. Migrations on boot. Structured JSON logs.

## Suggested build order

1. PostgreSQL, migrations, sqlc, store swap behind the existing interface.
2. Route-library script and procedural simulator for both fleets.
3. Simulated calendar, contracts, payer profiles, book lifecycle.
4. Block machine, outbox, unlock redelivery.
5. Policy, audit, HTTP routes, `openapi.yaml`.
6. Demo protection: rate limit, interval, self-heal.
7. Carteira UI against the design.
8. Compose + Caddy, VPS deploy, README.

## Risks that bend that order

1. Scope over the window: cut position history first; keep persistence, block machine, sim, Carteira, protection, deploy.
2. Low-quality routes make the map unbelievable: visually accept the seed before integrating.
3. Unbounded disk on the VPS: retention and the 40–60 book cap are mandatory from the first public deploy.
4. Visitors emptying or freezing the book: rate limit, interval, and self-heal ship with the first public deploy.
