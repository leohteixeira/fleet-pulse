---
title: 'Embedded broker, simulator, and telemetry logs'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'b0bc6635c05e143b67036e72786282a9ae662165'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md'
warnings: []
deferred:
  - summary: >-
      AGENTS.md and CLAUDE.md still describe the repository as license-only greenfield after the Go module exists.
    evidence: |-
      Both files say there is no Go module or application code. The change added go.mod, cmd/server, and internal packages. Fixing this edits agent-context files.
    location: >-
      AGENTS.md:11
    severity: low
  - summary: >-
      The mochi inline subscribe callback may hand ingest a payload slice that the broker later reuses.
    evidence: |-
      broker.Subscribe forwards pk.Payload without copying. Whether mochi recycles that buffer after the callback returns was not confirmed in source. A copy of the payload before handle() would settle it.
    location: >-
      internal/broker/broker.go:104
    severity: medium (unverified)
---

<intent-contract>

## Intent

**Problem:** The repository is greenfield (license and docs only). There is no Go module, no process, and no proof that vehicles can publish live positions through MQTT.

**Approach:** Stand up one Go module that starts an embedded mochi-mqtt v2 broker and 15–25 real MQTT vehicle clients. Online vehicles publish telemetry every 2 seconds; ingest logs each received VIN and position as slog JSON. No HTTP or UI.

## Boundaries & Constraints

**Always:**
- Module path `github.com/leohteixeira/fleet-pulse`, Go 1.26.
- Broker binds `0.0.0.0:1883`. Vehicles connect as real MQTT clients to that broker (not in-process publish helpers).
- 15–25 vehicles, each in its own goroutine; exactly one never connects and never publishes.
- Topic `fleet/{vin}/telemetry`. Payload is JSON with at least `vin`, `lat`, `lng` plus the assumed panel fields (`plate`, `model`, `battery`, `speed`, `heading`, `ignition`, `locked`, `odometer`, `trip`) and a short `displayId` (`V01`…), so later stories do not change the wire format.
- Ingest observes messages after they traverse the broker and logs `vin`, `lat`, `lng` with `log/slog` JSON.
- `context.Context` through I/O; cancel stops clients then the broker. No `utils` package. Interfaces declared by the consumer.
- Packages stay under `cmd/server` and `internal/` with short names.

**Never:**
- HTTP, SSE, store, UI, unlock/lock, geofence detection, or `openapi.yaml`.
- In-process simulator-to-ingest function calls that skip MQTT.
- A `utils`/`helpers` package, or interfaces declared by the implementing package.
- Logging secrets or treating presentation labels (`disponível`, `em uso`) as backend fields.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Happy path | Process starts with default config | Broker listens on `0.0.0.0:1883`; 15–25 vehicles exist; all but one publish JSON telemetry every 2s; ingest logs JSON lines containing `vin`, `lat`, `lng` | Broker or client start errors fail `main` and wrap the cause |
| Offline vehicle | Fleet includes the designated silent VIN | That VIN never connects and never appears in telemetry log lines | No error; silence is expected |
| Bad telemetry | Broker delivers a non-JSON or VIN-less payload | Ingest skips the message and logs a warning with the topic | Warning only; process stays up |
| Shutdown | Cancel the root context | Publishers disconnect, ingest unsubscribes, broker closes; no leaked goroutines | Close/disconnect errors are wrapped and logged once at the boundary |

</intent-contract>

## Code Map

Greenfield. HEAD `b0bc6635c05e143b67036e72786282a9ae662165` has no `go.mod`, `cmd/`, or `internal/`. `.gitignore` only ignores `_bmad/render/`. Remote is `github.com/leohteixeira/fleet-pulse`.

- `{project-root}/AGENTS.md` -- layout, ports, MQTT topics, slog JSON, no `utils`, consumer-owned interfaces
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md` -- start order broker → simulator → ingest; real MQTT clients; telemetry topic
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/SPEC.md` -- CAP-7 foundation, CAP-1 data path, Centro spawn later, one offline vehicle
- `{project-root}/.gitignore` -- extend with Go binaries, coverage, and local build artifacts
- `{project-root}/go.mod` -- create; module `github.com/leohteixeira/fleet-pulse`
- `{project-root}/cmd/server/main.go` -- create; `signal.NotifyContext`, JSON slog, wire broker → sim → ingest
- `{project-root}/internal/broker/broker.go` -- create; mochi-mqtt v2 TCP listener; implement ingest's `Subscriber`
- `{project-root}/internal/sim/sim.go` -- create; fleet factory, per-vehicle goroutine, paho (or equivalent) MQTT client
- `{project-root}/internal/ingest/ingest.go` -- create; declare `Subscriber`; parse telemetry; slog `vin`/`lat`/`lng`
- `{project-root}/internal/ingest/ingest_test.go` -- create; I/O matrix for parse/log/skip
- `{project-root}/internal/sim/sim_test.go` -- create; fleet size, one offline, 2s interval, JSON payload
- `{project-root}/internal/broker/broker_test.go` -- create; listen + subscribe round-trip on an ephemeral port

## Tasks & Acceptance

**Execution:**
- `{project-root}/go.mod` -- `go mod init github.com/leohteixeira/fleet-pulse` with Go 1.26; add mochi-mqtt server v2 and an MQTT client library; `go mod tidy` -- module and lockfile must exist before packages compile
- `{project-root}/.gitignore` -- ignore binaries, `coverage.out`, and local `bin/` -- keep build artifacts out of git
- `{project-root}/internal/ingest/ingest.go` -- declare `Subscriber` (filter + handler); `Run` parses JSON and logs `vin`, `lat`, `lng`; skip malformed payloads -- consumer owns the subscribe port
- `{project-root}/internal/ingest/ingest_test.go` -- unit-test the I/O matrix happy, bad-payload, and offline-absence cases with a fake `Subscriber` -- lock the log/parse surface without a broker
- `{project-root}/internal/broker/broker.go` -- embed mochi-mqtt v2, bind the given address (prod: `0.0.0.0:1883`), implement ingest `Subscriber` via the broker (inline subscribe or equivalent, no vendor types leaked) -- vehicles still connect over TCP
- `{project-root}/internal/broker/broker_test.go` -- start on an ephemeral port, publish through a real MQTT client, assert the subscriber handler fires -- proves the broker path
- `{project-root}/internal/sim/sim.go` -- build 20 vehicles (in 15–25) around Centro (`-23.55`, `-46.63`); one offline VIN never connects; others publish `fleet/{vin}/telemetry` every 2s on their own goroutine -- CAP-1 data path
- `{project-root}/internal/sim/sim_test.go` -- assert fleet size, exactly one offline, payload fields, and 2s period (use `testing/synctest` or a fake clock) -- keep the simulator deterministic
- `{project-root}/cmd/server/main.go` -- JSON slog, `signal.NotifyContext`, start broker then sim then ingest; default broker `0.0.0.0:1883` -- process-level wiring only

**Acceptance Criteria:**
- Given a clean checkout, when `go run ./cmd/server` starts, then the process listens on `0.0.0.0:1883` and stdout JSON logs include telemetry lines with `vin`, `lat`, and `lng` for online vehicles.
- Given the running simulator, when 4 seconds elapse, then each online VIN has produced at least two telemetry publishes on `fleet/{vin}/telemetry`.
- Given the designated offline VIN, when the process has been running long enough for two publish ticks, then no telemetry log line contains that VIN.
- Given a reachable vehicle client, when it publishes, then the log line is emitted by ingest after the message has traversed the broker — not by an in-process callback from `sim`.
- Given SIGINT/SIGTERM, when the root context cancels, then clients disconnect and the broker stops without hanging `main`.
- Given the tree, when `go test ./...` and `go test -race ./...` run, then both pass and there is no HTTP/UI package.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 35 findings — high 0, medium 8, low 12, false 11, maybe-false 4
- findings:
  - `[medium]` `[patch]` First telemetry can miss ingest Subscribe — started ingest first and wait on readySub before sim.Run
  - `[low]` `[patch]` ctx.Done drops errCh and Close hides runErr — drain errCh after Wait; Join Close with runErr
  - `[false]` `[reject]` sim declares publisher/mqttClient — unexported same-package test seams; no consumer sees them
  - `[low]` `[patch]` tautological offline ingest case — deleted the case
  - `[medium]` `[patch]` no ingest.Run + real broker slog test — added TestBroker_IngestLogsPublishedTelemetry
  - `[low]` `[defer]` AGENTS.md/CLAUDE.md still say greenfield — recorded in deferred; fix edits agent-context files
  - `[false]` `[reject]` no readiness log — success surface is telemetry lines with vin/lat/lng
  - `[maybe-false]` `[defer]` mochi payload slice reuse — unverified; recorded in deferred
  - `[low]` `[reject]` missing lat/lng logs as 0,0 — everyday vehicles always send both fields
  - `[low]` `[reject]` empty OnClientError — next 2s Publish surfaces the dead connection
  - `[low]` `[patch]` ./server not gitignored — ignore /server
  - `[false]` `[reject]` no GitHub Actions — not the process-log success surface
  - `[medium]` `[patch]` TelemetryFilter never asserted — TestRun now checks the recorded filter
  - `[low]` `[reject]` no goleak — adding TestMain plus vendor ignores is more than a deletion
  - `[false]` `[reject]` topic VIN vs JSON VIN — not a specified invariant
  - `[low]` `[reject]` Disconnect ignores context — paho Disconnect has no ctx
  - `[low]` `[patch]` errCh not drained (edge) — same drain/Join as above
  - `[medium]` `[patch]` sim before ingest (edge) — same readySub barrier
  - `[maybe-false]` `[reject]` QoS0 WriteTo can hang SIGINT — supervised run returned; local broker does not stick
  - `[low]` `[reject]` extra publish after cancel — one leftover log after SIGINT
  - `[false]` `[reject]` interval <= 0 panics NewTicker — production interval is 2s
  - `[false]` `[reject]` nil client and nil error — dialPaho and test stubs never return that pair
  - `[low]` `[reject]` OnClientError empty (edge) — same as the empty-callback reject above
  - `[false]` `[reject]` :: remapped to 127.0.0.1 — prod binds 0.0.0.0; waitReady succeeded
  - `[false]` `[reject]` Start twice or after Close — Start is called once per process
  - `[low]` `[reject]` whitespace-only VIN — simulator emits FPULSESAO%08d
  - `[maybe-false]` `[reject]` SIGINT hangs in Wait — 5s supervised run exited after INT
  - `[medium]` `[patch]` composed MQTT path untested (verification-gap) — TestBroker_IngestLogsPublishedTelemetry
  - `[medium]` `[patch]` subscribe filter untested (verification-gap) — TestRun asserts TelemetryFilter
  - `[medium]` `[patch]` publishLoop topic/payload untested — TestPublishLoop_Period now records both
  - `[medium]` `[patch]` parallel start other-finding — same readySub barrier
  - `[medium]` `[patch]` tests miss process stdout surface — composed slog test added; go run remains the supervised check
  - `[false]` `[reject]` offline vehicle expands the paragraph — valid super-set of the starting intent
  - `[false]` `[reject]` panel payload / Centro expand the paragraph — wire format for later stories
  - `[false]` `[reject]` ingest uses inline subscribe — vehicles remain real MQTT clients

## Design Notes

Use 20 vehicles so the count sits inside 15–25 without making the range a runtime flag. Spawn near the Centro rectangle so later geofence stories inherit realistic coordinates. Ingest's `Subscriber` is implemented by `internal/broker` so mochi types stay in that adapter. Vehicle clients must dial TCP; do not call broker publish APIs from `sim`.

## Verification

**Commands:**
- `go test ./...` -- expected: all packages pass
- `go test -race ./...` -- expected: no races
- `go run ./cmd/server` (short supervised run) -- expected: JSON logs with `vin`/`lat`/`lng`; broker accepts a TCP connection on 1883

## Auto Run Result

Status: done

Summary: One Go process embeds mochi-mqtt v2 on `0.0.0.0:1883`, runs 19 real Paho vehicle clients plus one offline VIN, and logs `vin`/`lat`/`lng` as slog JSON after the message crosses the broker.

Files changed:
- `go.mod` / `go.sum` — module `github.com/leohteixeira/fleet-pulse`, Go 1.26, mochi-mqtt v2, paho
- `.gitignore` — ignore binaries, coverage, `.gotmp/`, `/server`
- `cmd/server/main.go` — JSON slog, signal context, ingest-then-sim start, errCh drain
- `internal/broker` — embedded broker implementing `ingest.Subscriber`
- `internal/sim` — 20-vehicle fleet, 2s MQTT telemetry
- `internal/ingest` — parse/log/skip telemetry
- story spec — planning, review log, this result

Review findings: patched the subscribe-before-sim race, errCh/Close join, tautological offline case, composed broker+ingest slog test, TelemetryFilter assertion, publishLoop topic/payload, and `/server` gitignore. Deferred stale AGENTS/CLAUDE greenfield text and unverified mochi payload reuse. Rejected the remaining false/low/maybe-false items for the reasons in the triage log.

Follow-up review: true. Four medium entries were patched on the first pass. Unverified risk: `readySub` and `TestBroker_IngestLogsPublishedTelemetry` were not re-read by the hunter layers.

Verification:
- `go test ./...` — pass
- `go test -race ./...` — pass
- `go run ./cmd/server` (~5s, SIGINT) — 57 telemetry lines, 19 online VINs, min 3 ticks, offline VIN absent, listen `[::]:1883`

Residual risks: vehicles do not move yet; MQTT auth is allow-all; first-tick race is closed in process wiring but not re-reviewed.
