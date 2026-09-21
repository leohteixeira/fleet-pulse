---
id: SPEC-fleet-pulse
companions:
  - architecture.md
  - architecture-diagrams.md
  - state-machines.md
  - ux.md
  - ../../../docs/design/Fleet Pulse Spec.dc.html
  - ../../../docs/design/Fleet Pulse.dc.html
  - ../../../AGENTS.md
sources:
  - ../../../docs/fleet-pulse.md
  - ../../../docs/design/README.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents listed in frontmatter are for traceability — consult them only if you need narrative rationale or prose color this contract intentionally omits.

# Fleet Pulse MVP

## Why

A **vision to realize** and a **pain to solve**. A connected-fleet door command is not a synchronous call: the vehicle may be silent, late, or refuse. Treating it as request/response lies to the operator. Fleet Pulse exists so a technical interviewer can watch an honest command lifecycle and a live fleet on one URL. The narrative user is a fictional fleet operator; the real user is that interviewer. This is a short-window portfolio artifact, not a commercial product.

## Capabilities

- **CAP-1**
  - **intent:** An operator can see the current fleet on a map, with positions and derived vehicle states updating live.
  - **success:** Opening the demo URL shows 15–25 vehicles in motion; markers move from incoming telemetry without a page refresh; derived states include disponível, em uso, fora da área, and offline.

- **CAP-2**
  - **intent:** An operator can inspect one vehicle's current telemetry and command status.
  - **success:** Selecting a marker opens the designed panel; clearing selection returns the empty crosshair state. Offline speed renders as an em dash, not a stale number.

- **CAP-3**
  - **intent:** An operator can request an asynchronous door unlock and watch its lifecycle complete or fail in the open.
  - **success:** The request returns `202` with a command id; the UI walks `PENDING` → `SENT` → `ACKED|FAILED|TIMEOUT` inside 5 seconds of publish; repeating the same idempotency key does not create a second command. A distinct second command while one is in-flight is accepted `202` and starts when the first reaches a terminal state.

- **CAP-4**
  - **intent:** An operator can demonstrate the unhappy path when a vehicle never answers.
  - **success:** Unlocking or locking the permanently offline vehicle reaches `TIMEOUT` in 5 seconds with a clear expired message — no hang and no silent success.

- **CAP-5**
  - **intent:** An operator can see a vehicle refuse a command.
  - **success:** The injected ~10% execution failure yields `FAILED`, a short reason in the feed, and buttons that accept retry.

- **CAP-6**
  - **intent:** An operator can read a live, collapsible event feed of command outcomes and area-exit events.
  - **success:** Each outcome appears as one new row that slides in; no toast and no modal.

- **CAP-7**
  - **intent:** An evaluator can run the whole demo from one process with no required external service.
  - **success:** One binary serves HTTP, the embedded MQTT broker, the vehicle simulator, and the embedded UI at one URL.

- **CAP-8**
  - **intent:** An evaluator can follow one correlation id from the HTTP door command through the MQTT ack in structured logs.
  - **success:** The same id appears on the unlock or lock request, the published command, and the ack log line.

- **CAP-9**
  - **intent:** An operator can tell when the live stream is degraded and that positions are frozen, without being told the system has failed.
  - **success:** On SSE drop: amber reconnect pill with retry count, 2px dashed amber band at the top of the map, "positions frozen" notice, markers stop, commands wait. Nothing turns red.

- **CAP-10**
  - **intent:** An evaluator can read why the system is shaped this way and what the HTTP contract is.
  - **success:** The README has an architecture diagram and a trade-offs section; `openapi.yaml` documents the six HTTP endpoints.

- **CAP-11**
  - **intent:** An operator can request an asynchronous door lock and watch the same lifecycle as unlock.
  - **success:** `POST /api/vehicles/{vin}/lock` returns `202` with a command id; the UI walks the same states inside 5 seconds of publish; on `ACKED` the Portas row flashes to `TRAVADAS`. Queue rules are the same as unlock.

- **CAP-12**
  - **intent:** An operator can see when a vehicle leaves the allowed area.
  - **success:** Exiting the dashed Centro polygon shows `Fora da área` (amber marker and chip), increments the Fora KPI, and one area-exit row enters the feed.

## Constraints

- About four hours, one person. Map is functionally live before visual polish.
- Single process. No required external dependency. In-memory fleet store is accepted and must stay documented.
- Command expiry is **5 seconds from SENT** (MQTT publish). A queued `PENDING` does not burn that window. UI copy follows 5s, not the mock's 15s/6s.
- Door commands queue one-deep per vehicle: a distinct new POST while one is `PENDING` or `SENT` returns `202` and starts after the in-flight command terminals. A third new command while the slot is full returns `409` and the in-flight command. Same `Idempotency-Key` + vin + action still replays the existing record.
- Idempotency header is `Idempotency-Key`, scoped to vin + action.
- API and MQTT identify a vehicle by VIN. The snapshot also carries a short display id for the marker chip (`V07`).
- Simulated devices are real MQTT clients on embedded mochi-mqtt v2. Topics and HTTP surface: `architecture.md`. States and transitions: `state-machines.md`.
- SSE hub buffers per client and drops **oldest** events under pressure. Disable compression on the stream route.
- Frontend stack and state rules: `architecture.md`. Visual contract: `ux.md` plus the adopted design HTML. Rebuild a marker icon only when state, selection, or heading (rounded to 10°) changes; otherwise only `setLatLng`.
- Go: one module, packages by responsibility, no `utils` package, interfaces declared by the consumer, stdlib HTTP, `log/slog` JSON, `signal.NotifyContext` draining stream clients first.
- Bind `0.0.0.0`. Ports: HTTP 8300, web 3300, MQTT 1883.
- Authored repository text is English. Shipped UI copy is Portuguese, matching the design.
- If React 19 and react-leaflet v5 cannot be aligned, drive Leaflet imperatively from a `useRef`.
- Map tiles are Esri Canvas World Gray (dark and light) with the mock attribution.
- Dark default plus a light-theme toggle, as in the console mock.

## Non-goals

- Authentication, authorization, a database, multiple replicas, trip history, rental or billing, end-to-end tests, mobile responsiveness, cloud deploy.
- Prometheus ack latency, Redis last-known state, PostgreSQL/PostGIS history, NATS fan-out, generated TypeScript client.

## Success signal

The evaluator opens one URL, sees the fleet moving (including a vehicle leaving the Centro area), unlocks a reachable vehicle and watches the command reach a terminal state, locks it back, then unlocks the offline vehicle and sees a treated `TIMEOUT` with a clear message — all inside the 5-second command window. The evaluator can then walk the code line by line because modules stay small and the README names the trade-offs.

## Assumptions

- Telemetry carries the fields the designed panel shows: plate, model, battery, speed, heading, ignition, locked, odometer, trip, lat, lng, last-seen. The backend publishes telemetry, not presentation labels.
- Exactly one vehicle is permanently offline; command execution fails about 10% of the time on reachable vehicles.
- The allowed area is the mock Centro rectangle (south −23.585, north −23.525, west −46.685, east −46.60).
