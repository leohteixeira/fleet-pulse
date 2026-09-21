---
title: 'PostgreSQL, migrations, sqlc, and store swap'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'd0384365bed48c4ab5817c7445319c2a9a0452d1'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings:
  - oversized
deferred:
  - summary: >-
      AGENTS.md and CLAUDE.md still describe an in-memory store and go run
      without DATABASE_URL.
    evidence: |-
      This story asked only README and openapi.yaml. Agent-context files still
      say the MVP has no database, so a later docs pass should update them.
    location: >-
      AGENTS.md
    severity: low
---

<intent-contract>

## Intent

**Problem:** Phase-1 last-known fleet state lives only in `store.Memory`. A process restart wipes the roster and telemetry, so CAP-14 cannot hold.

**Approach:** Keep the consumer-owned `httpapi.Store` (`Snapshot`) and `command.Vehicles` (`Has`) ports. Replace the production backing store with PostgreSQL (pgx pool, goose migrations on boot, sqlc queries). Cache `vehicle_state` in memory in front of the upsert. Rental unlock/lock and SSE keep the same HTTP/MQTT behavior.

## Boundaries & Constraints

**Always:**
- Consumer-owned interfaces stay on HTTP and command. The store package does not export those ports.
- Production constructor opens a pgx pool from `DATABASE_URL`, runs embedded goose migrations, loads persisted rental rows into the cache, then seeds any missing `sim.NewFleet()` VINs (including the silent one).
- `Seed` / `Apply` / `Has` / `Snapshot` keep today's signatures so ingest, HTTP, and the 5s door machine do not change. Persist from `Apply`/`Seed` with a short timeout derived from the store lifecycle context.
- Schema matches architecture tables: `vehicles`, `vehicle_state`, `routes`, `customers`, `contracts`, `installments`, `payments`, `commands`, `audit_log`, `outbox`. `vehicles.fleet` is required (`rental` | `leasing`). This story reads and writes rental `vehicles` + `vehicle_state` only.
- `vehicle_state` is upserted and cached. Snapshot, crossing, and `Has` read the cache. After a successful persist + process restart, `GET /api/vehicles` still shows last-known fields for those VINs.
- `GET /healthz` is `200` only when the process is up and the pool pings. Database unreachable → non-`200`.
- Compose project name `fleet-pulse`. Postgres is for local/dev; do not publish 3300/8300/1883 from it. Host port for `go run` is `5435` (unassigned in the workspace matrix). No Caddy.
- Parameterized SQL only. No `utils` package. Close the pool on shutdown after SSE drain and broker close.

**Never:**
- Change the rental 5s `PENDING`/`SENT`/`ACKED`/`FAILED`/`TIMEOUT` machine, topics `fleet/{vin}/…`, one-deep queue, or SSE drop-oldest / no-gzip contract.
- Persist rental door commands, leasing rows, outbox relay, route seed, clock, contracts, or position history.
- Leave `store.New()` as the production wire in `cmd/server`. Do not declare store-owned interfaces for HTTP/ingest/command to import.
- Call an external router. Publish Fleet Pulse's assigned ports from the Postgres service.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fresh boot | `DATABASE_URL` set; empty database | Migrations create all ten tables; snapshot has 20 rental VINs including `FPULSESAO00000020`; Centro polygon unchanged | Missing/invalid DSN → process exits non-zero before HTTP listen |
| Telemetry persist | Valid ingest apply after seed | Cache + `vehicle_state` upsert; `GET /api/vehicles` shows new lat/lng | Persist error: log, keep cache+SSE; next apply retries |
| Restart | Apply lat/lng, stop process, start against same DB | Snapshot returns the persisted last-known fields for that VIN | Migration no-op when already applied |
| Health ready | Pool reachable | `GET /healthz` → `200` | None |
| Health not ready | Pool ping fails | `GET /healthz` → non-`200` | No stack leak; no DSN in logs |
| Unlock still works | Seeded VIN + `Idempotency-Key` | `POST /api/vehicles/{vin}/unlock` → `202` + command id; 5s machine unchanged | Unknown VIN still `404` via `Has` |
| SSE still works | Client on `/api/stream`; telemetry apply | `event: telemetry`; no gzip | Persist failure does not drop the SSE publish |

</intent-contract>

## Code Map

- `internal/store/store.go:49-146` -- `Memory` is today's concurrent cache: `Seed`, `Apply` (crossing), `Has`, `Snapshot`. Keep as the in-process cache; production must not wire it alone.
- `internal/store/store.go:10-16` -- `Centro` rectangle stays the snapshot polygon.
- `cmd/server/main.go:28-32` -- compile-time asserts `*store.Memory` implements `httpapi.Store` and `command.Vehicles`; retarget to the Postgres type.
- `cmd/server/main.go:54-84` -- `store.New()` + `Seed(rosterFromSim)` then HTTP; open DB first, migrate, load, seed, then broker/ingest/HTTP. Close pool in `orderlyShutdown` after broker.
- `cmd/server/main.go:195-240` -- `telemetrySink.Apply` calls `mem.Apply` then publishes SSE; keep that order and signatures.
- `internal/httpapi/httpapi.go:24-27` -- `Store` is `Snapshot()` only. Add a consumer-owned `Ready` port (`Ping(ctx) error`) so `healthz` can ping the pool without the store declaring HTTP types.
- `internal/httpapi/httpapi.go:116-118` -- `healthz` always `200` today; gate on DB ping.
- `internal/command/command.go:80-82` -- `Vehicles.Has` only; do not enlarge.
- `internal/store/store_test.go` -- cache/crossing unit tests stay Docker-free against the memory cache.
- `internal/httpapi/httpapi_test.go:24-91` -- `fakeStore` + healthz `200`; extend fake when ready is required, and add a failing-ping case.
- `internal/httpapi/httpapi_test.go:105+` -- SSE + unlock tests must keep passing against the same ports.
- `{project-root}/README.md:1-59` -- still documents "no database"; update the persistence trade-off and `DATABASE_URL` / compose.
- `{project-root}/openapi.yaml` `/healthz` -- document readiness (DB reachable).
- `{project-root}/.github/workflows/ci.yml:66-97` -- `go test -race ./...` must stay green; generated sqlc is committed so CI does not need the sqlc CLI. Integration tests that need Docker skip when unavailable.
- No `compose.yaml` or `migrations/` yet — create them. No `sqlc.yaml` yet.

## Tasks & Acceptance

**Execution:**
- `migrations/` -- goose SQL creating the ten architecture tables with `vehicles.fleet` and `vehicle_state` upsert key on VIN -- schema before queries
- `sqlc.yaml` + `internal/store/queries/` -- parameterized upsert/get/list for rental vehicles and vehicle_state only -- sqlc owns SQL, not string-built queries
- `internal/store/` -- Postgres type: open pool, embed+run goose, load cache, write-through Seed/Apply, Ping, Close; keep `Memory` as cache -- backing store swap
- `internal/httpapi/httpapi.go` -- healthz pings a consumer-owned ready port -- readiness
- `cmd/server/main.go` -- require `DATABASE_URL`, wire Postgres store, close pool on shutdown -- production no longer uses memory-only
- `compose.yaml` -- project `fleet-pulse`, Postgres 16, credentials in example env only, host `5435:5432` -- local `go run` can reach the DB
- `internal/store/*_test.go` -- keep cache unit tests; add Postgres persist+reload+migration test against real PostgreSQL -- CAP-14 evidence
- `internal/httpapi/httpapi_test.go` -- healthz ready / not-ready -- I/O matrix
- `openapi.yaml` + `README.md` -- healthz readiness and DATABASE_URL -- contract + trade-off
- `cmd/server/main.go` -- compile-time asserts retargeted -- callers still compile against consumer ports

**Acceptance Criteria:**
- Given an empty Postgres and a valid `DATABASE_URL`, when the process starts, then goose has created all ten named tables and `GET /api/vehicles` returns the Centro polygon plus 20 rental vehicles including `FPULSESAO00000020`.
- Given a seeded VIN whose last-known lat/lng was applied, when the process restarts against the same database, then `GET /api/vehicles` returns those persisted coordinates.
- Given a reachable pool, when `GET /healthz` is issued, then the status is `200`; given a failed ping, when `GET /healthz` is issued, then the status is not `200`.
- Given a seeded VIN and `Idempotency-Key`, when `POST /api/vehicles/{vin}/unlock` is issued, then the response is `202` and the command still expires 5s from `SENT`.
- Given an SSE client on `GET /api/stream`, when telemetry is applied, then the client receives `event: telemetry` without gzip and the snapshot cache matches the event VIN.
- Given no `DATABASE_URL`, when the process starts, then it exits before listening on `:8300`.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 26 findings — high 0, medium 8, low 9, false 9, maybe-false 0
- findings:
  - `[false]` `[reject]` persistCtx child of signal.NotifyContext aborts persist on SIGTERM — lifecycle cancel is the intended stop; persist must not outlive the process
  - `[low]` `[reject]` Apply persist delays SSE up to 2s — everyday upserts are milliseconds; a goroutine would add state this story does not need
  - `[low]` `[reject]` overlapping Apply can persist a stale lookup — the sim publishes one VIN sequentially; per-VIN persist queue is extra complexity
  - `[medium]` `[patch]` persistSeed failure leaves Has true so Apply hits vehicle_state FK — persistState now InsertRentalVehicle then upsert
  - `[low]` `[reject]` Apply does not persist plate/model onto vehicles — sim plate/model are fixed at seed
  - `[medium]` `[patch]` no test that persist error keeps the live Snapshot — added Close-then-Apply snapshot assertion
  - `[low]` `[reject]` healthz log omits err and nil Ready is untested — omitting err avoids DSN leak; production always passes Postgres
  - `[false]` `[reject]` goose migration has no Down — this story never rolls back; Down DROP would risk sqlc ingest
  - `[low]` `[patch]` compose interpolates POSTGRES_* with no default — now `${VAR:-fleetpulse}`
  - `[low]` `[defer]` AGENTS.md and CLAUDE.md still say no database — spec only required README/openapi
  - `[false]` `[reject]` future leasing tables lack FK indexes — unused this story
  - `[low]` `[reject]` unused Get* queries and unfiltered ListVehicleState — leasing not written yet; Gets are generated leftovers
  - `[false]` `[reject]` missing DATABASE_URL is only tested at the helper — run() returns that error before Listen
  - `[medium]` `[patch]` persistSeed failure leaves cache-only VINs — same persistState insert as the blind-hunter FK finding
  - `[medium]` `[patch]` Apply of a VIN missing from vehicles hits FK — persistState inserts the rental row first
  - `[low]` `[reject]` overlapping Apply stale persist — same as the concurrent-lookup finding
  - `[false]` `[reject]` persistCtx cancelled at SIGTERM — same lifecycle-cancel refutation
  - `[medium]` `[patch]` persistSeed Rollback used the persist ctx — now Rollback(context.Background())
  - `[false]` `[reject]` int32 overflow on battery/speed/heading — ingest values never approach that range
  - `[low]` `[patch]` compose without .env starts empty credentials — same defaults as the compose finding
  - `[medium]` `[patch]` Apply persist failure does not keep a live cache snapshot — Close-then-Apply test added
  - `[medium]` `[patch]` Postgres.Has never asserted — Seed now asserts seeded / UNKNOWN / empty
  - `[medium]` `[patch]` Postgres.Ping failure not observed — Ping after Close must error
  - `[false]` `[reject]` CAP-14 contracts/commands/audit not persisted — story intent is the phase-1 store swap, not the later book
  - `[false]` `[reject]` unlock/SSE tests still use Memory/fakes — production wires Postgres; Has/Ping/cache tests cover the new type
  - `[false]` `[reject]` Memory remains as cache — spec keeps it as the in-process cache; main no longer wires store.New()

## Auto Run Result

Status: done

Summary: Production store is PostgreSQL (pgx + goose + sqlc) with a Memory vehicle_state cache. `DATABASE_URL` is required. `/healthz` is ready only when the pool pings. Last-known rental positions survive process restart. Review patches: Apply inserts the vehicle before upserting state, seed rollback uses Background, compose has credential defaults, and persist/Has/Ping tests were added.

Files changed:
- `migrations/` — ten-table goose schema plus embed
- `sqlc.yaml`, `internal/store/queries/` — parameterized rental vehicle and vehicle_state SQL
- `internal/store/postgres.go` — Open/migrate/load/write-through/Ping/Close
- `internal/store/store.go` — cache lookup helper
- `internal/httpapi/httpapi.go` — Ready port and healthz ping
- `cmd/server/main.go` — require DSN, wire Postgres, close pool last
- `compose.yaml`, `env.example` — local Postgres on 5435
- `README.md`, `openapi.yaml` — persistence trade-off and readiness
- tests — persist/reload, Has, Ping after Close, persist-failure snapshot, healthz not-ready

Review: 6 patches applied (5 medium, 1 low). 1 deferred (AGENTS.md/CLAUDE.md). Rejected: lifecycle persist cancel, SSE delay, concurrent stale write, plate/model persist, healthz log, goose Down, unused indexes/queries, DSN-before-Listen, int32 overflow, and CAP-14/unlock-test/Memory-as-cache alignment notes.

Follow-up review recommended: true. Unverified residual: write-through still runs inside `Apply` before the SSE publish, so a slow upsert can delay the live feed; persist-on-Apply insert was not exercised against a failed seed in a live database.

Verification:
- `gofmt -l .` empty
- `go vet ./...` clean
- `go test ./...` pass (store persist/reload against Postgres 16 via testcontainers)
- `go test -race ./internal/store/` pass

Residual risks: rental door commands stay in process memory; `ListVehicleState` is not fleet-filtered yet; agent-context files still describe the no-database MVP.

## Design Notes

Write-through: update the memory cache first (SSE and crossing stay synchronous), then upsert `vehicle_state` (and insert `vehicles` on seed). A persist error is logged and not returned from `Apply` so ingest's `Sink` signature stays `Apply(Telemetry)`.

`store.Memory` remains the cache implementation used by the Postgres type. Production `main` must not call `store.New()` as the HTTP/command dependency.

Rental door commands stay in `internal/command` memory. Empty `commands` / `outbox` / leasing tables exist for later stories.

Example `DATABASE_URL`: `postgres://fleetpulse:fleetpulse@127.0.0.1:5435/fleetpulse?sslmode=disable`

## Verification

**Commands:**
- `go test ./...` -- expected: cache, HTTP, command, and (when Docker is available) persist/reload tests pass
- `go test -race ./...` -- expected: no races on cache + write-through
- `gofmt -l .` -- expected: empty
- `go vet ./...` -- expected: clean

**Manual checks (if no CLI):**
- `docker compose up -d` then `DATABASE_URL=... go run ./cmd/server`; curl `/healthz`, `/api/vehicles`; apply telemetry; restart; curl snapshot still has last lat/lng; `POST` unlock still `202`
