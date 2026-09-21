# Architecture

Runtime topology and contracts. Diagrams: `architecture-diagrams.md`. Command machine: `state-machines.md`. Repo conventions that agents must follow: `AGENTS.md`.

## Process

One Go process, one module.

| Surface | Owner | Port | Bind |
|---|---|---:|---|
| HTTP API + embedded UI | `cmd/server` | 8300 (API), 3300 (Vite / embedded web) | `0.0.0.0` |
| MQTT broker | embedded mochi-mqtt server v2 | 1883 | `0.0.0.0` |

The process starts the broker, then the simulator, then the ingest/store, then HTTP. Simulated vehicles connect as **real MQTT clients**, not in-process function calls.

Layout:

```text
cmd/server/
internal/          # short names, by responsibility, no cycles, no utils
web/               # Vite app; production build embedded with go:embed
openapi.yaml
```

Interfaces are small and declared by the **consuming** package.

## Simulator

- Instantiates 15–25 vehicles, each in its own goroutine.
- Publishes telemetry on `fleet/{vin}/telemetry` every 2 seconds.
- One vehicle is permanently offline (never connects / never acks).
- Reachable vehicles fail command execution about 10% of the time and publish a failed ack.
- Paths may cross the allowed Centro polygon so CAP-12 is demonstrable.

## Store

In-memory fleet + command store behind an interface. Concurrent reads. Conscious MVP trade-off; document it in the README. No persistence.

Telemetry updates last-known vehicle fields, including VIN and a short display id. Command records hold id, vin, action (`unlock`|`lock`), state, correlation id, idempotency key, timestamps, and an optional successor id when a command is queued.

The store owns the allowed polygon. On each telemetry point it detects an inside→outside crossing and records an area-exit event for SSE. It does not store presentation labels.

## MQTT

| Topic | Publisher | Payload role |
|---|---|---|
| `fleet/{vin}/telemetry` | device | last-known position and vehicle fields |
| `fleet/{vin}/commands` | server | unlock or lock |
| `fleet/{vin}/ack` | device | accepted or refused execution |

Ingest subscribes to telemetry and ack. A door command publishes when it becomes the in-flight command: immediately if the vehicle is idle, otherwise when the predecessor reaches a terminal state.

## HTTP

Document in `openapi.yaml`.

| Method | Path | Contract |
|---|---|---|
| `GET` | `/api/vehicles` | Fleet snapshot for first paint. Includes the allowed polygon so the frontend can draw it and derive `fora`. |
| `GET` | `/api/stream` | SSE. Per-client buffer; drop **oldest** under pressure. No compression on this route. Events: telemetry patches, command-state changes, area-exit. |
| `POST` | `/api/vehicles/{vin}/unlock` | Header `Idempotency-Key`. `202` + command id. Same key + vin + action returns the original command. A distinct new command while one is in-flight is queued (`202`); a third while the slot is full is `409` + the in-flight command. |
| `POST` | `/api/vehicles/{vin}/lock` | Same machine, idempotency, and queue rules as unlock. |
| `GET` | `/api/commands/{id}` | Current command state. |
| `GET` | `/healthz` | Liveness. |

Frontend talks to the backend only through this contract. Business rules, command expiry, and geofence crossing stay on the server. The frontend derives presentation labels (`disponível`, `em uso`, `fora`, `offline`) from telemetry plus the published polygon.

## Streaming

The frontend keeps one `EventSource`, guarded against React 19 StrictMode double mount. Fleet client state is a `useReducer` indexed by VIN.

On disconnect: reconnect with a visible retry count; freeze marker motion; do not send unlock or lock until the stream is live again (CAP-9).

## Frontend build

React 19, TypeScript, Vite, react-leaflet v5, CSS or CSS modules. No router, no state library, no component library. Production build is `go:embed`'d; unknown paths fall back to `index.html`.

Mock logic inside `<script type="text/x-dc">` (generation, movement, command outcomes, stream drops) does **not** ship. The browser only renders and selects.

Theme: dark default, light via `data-theme="light"` on `:root`, toggle in the header. Persist the choice in `localStorage` key `fleetpulse-theme` as the mock does.

Tiles: Esri Canvas `World_Dark_Gray_Base` / `World_Light_Gray_Base`, attribution `© Esri, HERE, OpenStreetMap`.

## Logs and shutdown

`log/slog` JSON. Propagate one correlation id from the unlock or lock HTTP request through the MQTT publish to the ack line (CAP-8). Never log secrets.

Shutdown: `signal.NotifyContext`. Drain SSE clients first, then stop the simulator, then close the broker, then exit.

## Packaging

CAP-7: `go run ./cmd/server` (and later a single binary) is enough. Vite is a development convenience; the evaluator path is the embedded UI.

## Suggested build order

1. Broker, simulator, telemetry in logs.
2. Store, snapshot, SSE — prove with curl / CLI.
3. Map with live markers (functional before polish).
4. Unlock path, state machine, 5s expiry; then lock as the sibling POST.
5. Panel, feed, offline timeout, 10% refuse, geofence crossing.
6. README diagram + trade-offs, `openapi.yaml`, embed, theme toggle.

## Risks that bend that order

1. Map polish last. Live markers before visual fidelity.
2. React 19 + react-leaflet peer conflict: install aligned versions together; fall back to imperative Leaflet in a `useRef`.
3. Proxy buffering of SSE: disable compression on `/api/stream`.
