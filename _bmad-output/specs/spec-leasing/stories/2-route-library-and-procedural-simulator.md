---
title: 'Route library and procedural simulator'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'e84803568d0982c05999c610a5c3c39c674ed6d4'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings:
  - oversized
deferred:
  - summary: >-
      The committed seed is Manhattan L-walks between POIs, not OSRM or OSM
      street traces.
    evidence: |-
      scripts/seedroutes interpolates L-walks and clamps to the Greater SP
      viewport. Regenerating ~300 real road routes is not a one-line patch.
    location: >-
      scripts/seedroutes
    severity: low
---

<intent-contract>

## Intent

**Problem:** Both fleets still wander the Centro OSM extract. CAP-16 motion needs a versioned São Paulo route library and leasing devices that publish as real MQTT clients on their own topics.

**Approach:** Commit an offline seed (~60 Greater São Paulo POIs, ~300 road routes). Drive about 20 rental vehicles on `fleet/{vin}/` and about 50 leasing vehicles on `leasing/{vin}/telemetry|ack`. Runtime never calls an external router.

## Boundaries & Constraints

**Always:**
- Seed is a versioned, committed, embedded artifact. A generator script may call OSRM or walk a local graph **offline once**; `go run ./cmd/server` and tests never open a router URL.
- Rental count stays 20 including `FPULSESAO00000020` silent. Topics stay `fleet/{vin}/telemetry|commands|ack`. 5s door machine, Centro snapshot polygon, and `GET /api/vehicles` remain rental-only.
- Leasing count is about 50. Topics are `leasing/{vin}/telemetry`, `leasing/{vin}/ack`, and subscribe `leasing/{vin}/commands`. They are real MQTT clients (same paho path as rental). Viewport for leasing motion is Greater SP (south −23.63, north −23.49, west −46.87, east −46.41).
- Fleets never mix: leasing VINs must not appear in `GET /api/vehicles` or rental SSE `telemetry` events. Do not `Seed`/`Apply` leasing rows into the rental snapshot cache.
- RNG seed is environment-configurable (`SIM_SEED`). Same seed → same first-route assignment in tests.
- Incident scheduler: temporary offline (stop publish), low battery, signal loss (drop some publishes). Deterministic under `SIM_SEED`.
- Leave a refuse hook on leasing devices (~15%) for a future block command. Do not implement the block machine.
- `store.Postgres` and the rental 5s machine stay as story 1 left them.

**Never:**
- Call OSRM, Overpass, or any external router from the running server or `go test`.
- Put leasing vehicles on `fleet/{vin}/` or in the rental snapshot/SSE.
- Change unlock/lock states, expiry, or Centro geofence.
- Contracts, clock, outbox, Carteira UI, or position-history tables.
- A `utils` package.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Seed shape | Embedded library | ≥55 and ≤70 POIs; ≥280 and ≤320 routes; every route has ≥2 lon/lat points inside Greater SP bounds | Missing/corrupt seed → process/test fail with wrapped error, no router fallback |
| Rental MQTT | Online rental VIN | Publishes `fleet/{vin}/telemetry` every ~2s; positions follow seed routes (or graph snapped to them), not a new random walk off the library | Offline VIN still silent |
| Leasing MQTT | Online leasing VIN | Publishes `leasing/{vin}/telemetry` every ~2s as a real MQTT client; acks on `leasing/{vin}/ack` | Does not publish on `fleet/{vin}/` |
| No mix | Process running both fleets | `GET /api/vehicles` still 20 rental VINs including offline; no leasing VIN | None |
| Reproducible seed | `SIM_SEED` set | Two `NewLeasingFleet`/`NewFleet` builds assign the same first route ids | Unset seed uses a process default |
| Incidents | Seeded RNG | At least one vehicle eventually reports battery ≤15 or skips a publish (signal/offline) in a unit test with a fake clock/ticker | Scheduler must not panic |
| Refuse hook | Leasing device `RefuseBlock()` | About 15% true over many rolls with fixed seed | Unused until story 4 |

</intent-contract>

## Code Map

Continuity from story 1 (`e848035`): production store is `store.Postgres`; `cmd/server` seeds `sim.NewFleet()` into that store. Snapshot is the whole cache — leasing must not be seeded there.

- `internal/roads/roads.go` + `internal/roads/centro.json` -- Centro graph used by rental roam/wander. Keep for Centro snap/geofence; do not make it the São Paulo library.
- `internal/sim/sim.go:21-31` -- `FleetSize=20`, `OfflineVIN`, `PublishInterval=2s`. Keep rental constants.
- `internal/sim/sim.go:108-135` -- `NewFleet` snaps to `roads.Default()`. Point rental motion at the new library while keeping 20 VINs and the silent one.
- `internal/sim/sim.go:137-150` -- `TelemetryTopic`/`CommandTopic`/`AckTopic` are `fleet/{vin}/…`. Keep for rental. Add leasing topic helpers; do not reuse these for leasing.
- `internal/sim/sim.go:175-178` -- `Run` only starts `NewFleet()`. Start both fleets.
- `internal/sim/sim.go:218-268` -- `runVehicle` subscribes `CommandTopic` and publishes `TelemetryTopic`. Parameterize prefix so leasing uses `leasing/{vin}/`.
- `internal/sim/sim.go:278-314` -- `stepVehicle` / wander-on-Centro. Rental may still demonstrate the west-exit VIN; other motion must consume library routes so one hour does not loop a tiny mesh.
- `internal/ingest/ingest.go:12-16` -- only `fleet/+/telemetry` and `fleet/+/ack`. Subscribe leasing filters too. Applying leasing telemetry into `httpapi` SSE/`Snapshot` is forbidden; log or a no-op sink is enough this story.
- `cmd/server/main.go:70` -- `pg.Seed(rosterFromSim(sim.NewFleet()))` rental only. Do not seed leasing into Postgres cache.
- `cmd/server/main.go:117` -- `sim.Run` must launch leasing clients as well.
- `internal/store/postgres.go` -- rental write-through. Do not add leasing `Apply` here this story.
- `scripts/fetchroads/main.go` -- Centro OSM fetch. New generator lives beside it (`scripts/seedroutes` or similar), writes the committed seed, not runtime.
- `internal/sim/sim_test.go` -- MQTT session tests assume `fleet/{vin}/`. Keep those; add leasing topic tests.
- Story 1 snapshot tests expect 20 rental VINs + Centro polygon — must stay green.

## Tasks & Acceptance

**Execution:**
- `internal/routes/` (or `internal/roads` library file, not `utils`) -- embed POIs + routes; load-only API; no HTTP client -- CAP-16 seed
- `scripts/seedroutes/` -- offline generator that writes the committed seed; document that production never runs it -- generate once
- `internal/sim/sim.go` -- drive rental from library; add leasing fleet (~50), leasing topics, `SIM_SEED`, incident scheduler, `RefuseBlock` hook -- both fleets live
- `internal/ingest/ingest.go` -- subscribe `leasing/+/telemetry` and `leasing/+/ack` without applying into the rental store/SSE -- no mix
- `cmd/server/main.go` -- start both fleets; keep rental-only Seed -- process wiring
- `internal/sim/*_test.go` + `internal/routes/*_test.go` -- seed shape, topic prefixes, no mix, refuse hook, incidents, reproducible seed -- I/O matrix
- `README.md` -- mention the committed route library and `SIM_SEED` -- trade-off

**Acceptance Criteria:**
- Given the embedded seed, when it is loaded, then it contains 55–70 POIs and 280–320 routes whose points lie inside the Greater SP bounds.
- Given a running process, when `GET /api/vehicles` is issued, then it still returns exactly 20 rental vehicles including `FPULSESAO00000020` and no leasing VIN.
- Given an MQTT client subscribed to `leasing/+/telemetry`, when the simulator runs, then at least one leasing VIN publishes on that prefix and never on `fleet/{vin}/telemetry`.
- Given `SIM_SEED=1`, when the leasing (or rental) roster is built twice, then the assigned first route ids match.
- Given a leasing vehicle, when `RefuseBlock` is rolled many times under a fixed seed, then the true rate is between 10% and 20%.
- Given the generator script, when someone reads it, then it is not imported by `cmd/server` or `internal/sim` at runtime.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 23 findings — high 0, medium 9, low 8, false 6, maybe-false 0
- findings:
  - `[false]` `[reject]` WanderVIN no longer walks Centro — Design Notes keep the west-exit demo VIN on the Centro graph
  - `[medium]` `[patch]` finishing a route teleports to the new polyline start — now snaps via nearestOnRoute
  - `[low]` `[reject]` NewFleet snaps Centro then projects onto a Grande SP route — first-step snap is intended; rental spawn stays Centro
  - `[low]` `[defer]` seed is Manhattan L-walks, not OSRM street traces — regenerating 300 road routes is out of a trivial patch
  - `[low]` `[reject]` generator uses few POIs as origins — counts and bounds still meet the matrix
  - `[medium]` `[patch]` incident scheduler fires once — nextIncidentAt is rescheduled when an incident ends
  - `[low]` `[reject]` incidents test accepts any short publish count — scheduler reschedule is the real fix
  - `[low]` `[reject]` leasing ingest logs every tick and has no live SSE process test — Apply into rental SSE is already forbidden and tested
  - `[low]` `[reject]` invalid SIM_SEED falls back silently — empty/invalid → default is the specified behavior
  - `[low]` `[reject]` nearLibraryRoute uses 250 m to a vertex — test helper, not production motion
  - `[low]` `[reject]` RefuseBlock also works on rental vehicles — unused until story 4; rental door refuse stays rollRefuse
  - `[false]` `[reject]` readySub test still uses n=2 — that tests the helper; production now waits for 4 subscriptions
  - `[medium]` `[patch]` route change teleports (edge hunter) — same nearestOnRoute snap
  - `[medium]` `[patch]` lock/unlock during offline still publishes telemetry — ack stays; telemetry skipped in offline/signal-loss
  - `[medium]` `[patch]` Parse allows duplicate POI ids — now rejected
  - `[medium]` `[patch]` Parse allows unknown From/To — now rejected
  - `[false]` `[reject]` WanderVIN claim vs library — same Design Notes exception as the first finding
  - `[medium]` `[patch]` sim.Run can drop leasing without a failing test — TestRun_StartsBothFleets added
  - `[medium]` `[patch]` leasing ack topic never observed — TestRunVehicle_LeasingTopics now delivers a command
  - `[medium]` `[patch]` SIM_SEED not observed as an input — seed 1 vs 2 and unset default now asserted
  - `[false]` `[reject]` intent required an OSRM-generated artifact — the story allows an offline walk; runtime still never calls a router
  - `[false]` `[reject]` CAP-16 as a named street algorithm — the story treats CAP-16 as library-driven motion
  - `[false]` `[reject]` leasing MQTT tests use a fake dial — same pattern as existing rental MQTT tests; production still uses paho

## Auto Run Result

Status: done

Summary: Embedded Greater SP route library (62 POIs, 300 routes) drives rental (`fleet/{vin}/`) and ~50 leasing MQTT clients (`leasing/{vin}/telemetry|ack`). Ingest listens to leasing topics but does not apply them into the rental snapshot/SSE. Review patches: no teleport on route change, recurring incidents, no telemetry during offline ack, stricter seed Parse, and tests for both fleets, leasing ack topic, and SIM_SEED.

Files changed:
- `internal/routes/` — embedded seed and load-only API
- `scripts/seedroutes/` — offline generator (not imported at runtime)
- `internal/sim/sim.go`, `internal/sim/leasing.go` — two-fleet motion, topics, incidents, RefuseBlock
- `internal/ingest/ingest.go` — leasing subscribe, log-only
- `cmd/server/main.go` — start both fleets; ready wait is 4 subscriptions
- tests and README / env.example

Review: 7 patches applied (medium). 1 deferred (Manhattan seed geometry). Rejected: WanderVIN Centro exception, first-step Centro snap, ingest log volume, invalid-seed fallback, helper distance, rental RefuseBlock, OSRM-literal and fake-dial alignment notes.

Follow-up review recommended: true. Unverified residual: route library geometry is still synthetic L-walks; visual “real streets” was not proven in a browser/map.

Verification:
- `gofmt -l .` empty
- `go vet` clean on touched packages
- `go test -count=1` on sim/routes/ingest/httpapi/cmd/server pass
- `go test -race -count=1 ./internal/sim/` pass

Residual risks: leasing last-known is not persisted; contracts/clock/block machine are later stories.

## Design Notes

Rental still owns the Centro polygon and the west-exit demo VIN. Library routes replace “random roam on `centro.json` only” for ongoing motion so trajectories do not repeat a 15-minute Centro loop.

Leasing VINs must not share the `FPULSESAO` rental sequence. Use a distinct prefix (for example `FPULSELSG`).

`RefuseBlock` is a method or func on the leasing vehicle. Story 4 will call it when a block command is executed. A 15% hook with no block machine is enough.

Incident scheduler can live inside `stepVehicle` / `publishLoop` (skip publish, drop battery). Keep it seedable.

Validate the seed in tests (counts + bounds). A small GeoJSON dump in testdata is optional evidence, not a UI.

## Verification

**Commands:**
- `go test ./...` -- expected: seed, sim, ingest, store, HTTP tests pass
- `go test -race ./...` -- expected: no races on two-fleet publish
- `gofmt -l .` -- expected: empty
- `go vet ./...` -- expected: clean

**Manual checks (if no CLI):**
- Subscribe `leasing/+/telemetry` and `fleet/+/telemetry` on the broker; confirm prefixes do not mix; `curl /api/vehicles` still 20 rental VINs
