---
title: 'In-memory store, snapshot, and SSE'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'c3b88fd192a8d1fd2e0345a14a313c9076bc29f9'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/stories/1-embedded-broker-simulator-and-telemetry-logs.md'
warnings: []
deferred:
  - summary: >-
      Snapshot and store do not record last-seen timestamps from ingest time.
    evidence: |-
      SPEC assumptions list last-seen on the panel. Story 2 success is polygon + display ids via curl; last-seen belongs to later panel/offline work.
    location: >-
      internal/store/store.go:27
    severity: low
  - summary: >-
      Shutdown cancels HTTP and the simulator together instead of draining SSE clients first.
    evidence: |-
      Architecture and story 6 require draining stream clients first. Story 2 only required closing HTTP then the broker.
    location: >-
      cmd/server/main.go:87
    severity: low
---

<intent-contract>

## Intent

**Problem:** Telemetry only appears in process logs. There is no last-known fleet state and no HTTP surface, so CAP-1 data and CAP-9 stream plumbing cannot be proven with curl.

**Approach:** Add an in-memory fleet store, seed it with the existing 20-vehicle roster (including the offline VIN), apply ingest telemetry to last-known fields, and expose `GET /api/vehicles`, `GET /api/stream`, and `GET /healthz` on `0.0.0.0:8300`.

## Boundaries & Constraints

**Always:**
- Store is in-memory, concurrent-safe, behind interfaces declared by consumers (HTTP and ingest). No vendor types on those ports.
- Snapshot JSON includes the Centro rectangle (south −23.585, north −23.525, west −46.685, east −46.60) and every vehicle with `vin` plus `displayId`. VIN is the API identifier.
- Seed the store from `sim.NewFleet()` so the silent VIN is present before any publish.
- Ingest keeps logging `vin`/`lat`/`lng` and also applies a successful parse to the store. Malformed payloads still skip with a warning.
- `GET /api/stream` is SSE, one bounded buffer per client, drop **oldest** under pressure, no compression on that route.
- SSE events for this story are telemetry patches (vin + last-known fields). Bind HTTP to `0.0.0.0:8300`. Stdlib `net/http`.
- `GET /healthz` is liveness (`200`).

**Never:**
- Persistence, UI, unlock/lock, command records, area-exit detection, `openapi.yaml`, or presentation labels (`disponível`, `em uso`, `fora`, `offline`).
- A `utils` package, or interfaces declared by the store package for HTTP/ingest to import.
- Compressing `/api/stream` or buffering the stream as a single growing slice that drops newest.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Snapshot | Process running; fleet seeded | `GET /api/vehicles` is `200` JSON with Centro polygon and 20 vehicles, each with `vin` and `displayId`, including `FPULSESAO00000020` | Encode errors → `500`, wrapped at the handler |
| Health | Process listening | `GET /healthz` is `200` | None |
| Telemetry apply | Valid MQTT telemetry after ingest parse | Store last-known fields update; snapshot reflects new `lat`/`lng`; SSE clients receive a telemetry event for that VIN | Parse failures stay warn-and-skip; store unchanged |
| SSE drop-oldest | Client buffer full, another event arrives | Oldest queued event is discarded; newest is kept | No error to the publisher |
| SSE no compression | `GET /api/stream` | `Content-Type` is `text/event-stream`; response is not gzip-compressed | None |
| Unknown method | `POST /api/vehicles` | `405` | None |

</intent-contract>

## Code Map

Continuity from story 1 (`c3b88fd`). Process today: broker → ingest subscribe → sim. Ingest only logs; no HTTP, no store.

- `cmd/server/main.go:33-83` -- wire HTTP after ingest is subscribed; keep readySub; drain errors; close HTTP then broker
- `internal/ingest/ingest.go:32-62` -- `Run` must accept a consumer-declared sink and apply parsed telemetry; keep slog
- `internal/ingest/ingest.go:64-72` -- `parse` only checks VIN; expand the applied struct to the wire fields already in `sim.EncodeTelemetry`
- `internal/sim/sim.go:87-111` -- `NewFleet` is the seed roster, including `OfflineVIN`
- `internal/sim/sim.go:16-27` -- `FleetSize`, Centro spawn, `OfflineVIN`
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/SPEC.md` -- Centro bounds; offline vehicle; snapshot polygon
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md:35-64` -- in-memory store, snapshot + SSE + healthz; drop oldest
- `internal/store/store.go` -- create; memory map by VIN, polygon, Snapshot, Apply; no exported consumer interfaces
- `internal/httpapi/httpapi.go` -- create; declare `Store` (snapshot) and hub ports; mux; bind `0.0.0.0:8300`
- `internal/httpapi/sse.go` -- create; per-client ring, drop oldest, disable compression
- `internal/httpapi/httpapi_test.go` -- create; snapshot, healthz, SSE event, drop-oldest, no gzip
- `internal/store/store_test.go` -- create; seed, apply, snapshot polygon + offline VIN
- `internal/ingest/ingest_test.go` -- extend; apply on happy path, skip does not apply

## Tasks & Acceptance

**Execution:**
- `internal/store/store.go` -- in-memory fleet keyed by VIN, Centro polygon, `Seed`, `Apply`, `Snapshot` -- last-known state for HTTP and ingest
- `internal/store/store_test.go` -- seed 20 vehicles, offline VIN present, apply updates lat/lng, snapshot includes polygon -- lock store behavior
- `internal/httpapi/httpapi.go` -- declare `Store` with `Snapshot`; `GET /api/vehicles`, `GET /healthz`; listen `0.0.0.0:8300` -- consumer owns the read port
- `internal/httpapi/sse.go` -- `GET /api/stream` SSE; bounded per-client buffer; drop oldest; no compression -- CAP-9 plumbing
- `internal/httpapi/httpapi_test.go` -- unit-test the I/O matrix (snapshot, health, SSE event, drop-oldest, no gzip, 405) -- curl-equivalent without a live process
- `internal/ingest/ingest.go` -- declare a small `Sink` (`Apply` parsed telemetry); call it after successful parse -- ingest consumes the store, not the reverse
- `internal/ingest/ingest_test.go` -- happy path applies; bad payload does not -- keep skip-vs-apply split
- `cmd/server/main.go` -- seed store from `sim.NewFleet()`, pass sink into ingest, start HTTP after subscribe, shut down the listener on cancel -- process wiring only

**Acceptance Criteria:**
- Given a running process, when `curl -sS http://127.0.0.1:8300/api/vehicles` is issued, then the body is JSON with the Centro polygon and 20 vehicles including `displayId` and the offline VIN.
- Given a running process, when `curl -sS http://127.0.0.1:8300/healthz` is issued, then the status is `200`.
- Given an SSE client on `/api/stream`, when an online vehicle publishes telemetry, then a telemetry event for that VIN arrives without gzip encoding.
- Given a full per-client SSE buffer, when another event is published, then the oldest queued event is dropped and the newest remains.
- Given two seconds of runtime after seed, when the snapshot is read again, then at least one online VIN has a last-known position that matches a published telemetry point.
- Given the tree, when `go test ./...` and `go test -race ./...` run, then both pass.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 21 findings — high 0, medium 5, low 7, false 7, maybe-false 2
- findings:
  - `[low]` `[reject]` Apply wipes last-known on partial JSON — everyday sim publishes full EncodeTelemetry
  - `[false]` `[reject]` last-seen missing — not the curl snapshot success surface; deferred
  - `[maybe-false]` `[reject]` drop-oldest races the SSE reader — buffer 32 + 2s ticks; extra drop still newest-preserving
  - `[false]` `[reject]` httpapi.Store returns store.Snapshot — no user-facing defect
  - `[false]` `[reject]` SSE not drained before sim stop — story 6; deferred
  - `[medium]` `[patch]` telemetrySink untested — added TestTelemetrySink_Apply
  - `[false]` `[reject]` README in-memory trade-off — story 6
  - `[low]` `[reject]` Apply inserts unknown VIN — only the seeded roster publishes
  - `[low]` `[reject]` gzip test uses DisableCompression — live curl had no Content-Encoding gzip
  - `[low]` `[reject]` nil Store panics — New is always called with mem
  - `[low]` `[reject]` Inf lat/lng — sim does not emit non-finite numbers
  - `[maybe-false]` `[reject]` writeSSE ignore cancel — supervised SIGINT released :8300
  - `[medium]` `[patch]` ingest happy path omits wire fields — full EncodeTelemetry payload asserted
  - `[medium]` `[patch]` Centro tests compare Centro to itself — four literals asserted
  - `[medium]` `[patch]` empty DisplayID preserve untested — Apply with empty id keeps seed
  - `[low]` `[reject]` Apply zeros numeric fields on partial JSON — same everyday full payload
  - `[false]` `[reject]` unknown VIN (edge) — roster-only publishers
  - `[low]` `[reject]` nil Store (edge) — unreachable from main
  - `[medium]` `[patch]` process sink untested (verification-gap) — same main_test
  - `[false]` `[reject]` curl-vs-httptest surface gap — live curl on this pass confirmed the three routes
  - `[false]` `[reject]` last-seen / drain expansions — not in the starting paragraph

## Design Notes

Keep package name `httpapi` so it does not stutter with `net/http`. Snapshot vehicles may be an array; VIN remains the identifier inside each object. SSE event name `telemetry` with JSON data is enough for curl and for story 3. Buffer size can stay an unexported constant (32). Do not detect geofence crossings yet — the polygon is published only.

## Verification

**Commands:**
- `go test ./...` -- expected: all packages pass
- `go test -race ./...` -- expected: no races
- `go run ./cmd/server` plus curl -- expected: `/healthz` 200; `/api/vehicles` has polygon + 20 VINs; `/api/stream` emits `event: telemetry` without `Content-Encoding: gzip`

## Auto Run Result

Status: done

Summary: In-memory store seeded from the 20-vehicle roster, ingest applies last-known fields, and HTTP on `0.0.0.0:8300` serves snapshot, SSE (drop-oldest, no gzip), and healthz.

Files changed:
- `internal/store` — concurrent map, Centro polygon, Seed/Apply/Snapshot
- `internal/httpapi` — consumer Store/HubPort, vehicles/healthz/stream
- `internal/ingest` — Sink + full Telemetry parse
- `cmd/server` — seed, telemetrySink, listen 8300
- tests and story spec

Review findings: patched telemetrySink test, full ingest wire payload, Centro literals, and empty-DisplayID preserve. Deferred last-seen and SSE-first drain. Rejected the rest per the triage log.

Follow-up review: true. Four medium entries were patched. Unverified risk: the new `cmd/server` sink test was not re-read by the hunter layers.

Verification:
- `go test ./...` and `go test -race ./...` — pass
- curl: `/healthz` 200; 20 vehicles + Centro + offline VIN at spawn lat; SSE `event: telemetry` without gzip

