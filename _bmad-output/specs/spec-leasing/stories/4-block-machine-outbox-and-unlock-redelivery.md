---
title: 'Block machine, outbox, and unlock redelivery'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'f82f8009fd77adf92f02c72429093bef16033374'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/state-machines.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings:
  - oversized
deferred:
  - summary: >-
      After restart, NewLeasingFleet starts unblocked even when Load restored ACKED flags, so a blocked car can move until a new command is acked.
    evidence: |-
      sim.Run always builds a fresh roster with blocked=false. Wiring Blocked() into Run would add a new public surface.
    location: >-
      internal/sim/sim.go:237
    severity: medium
---

<intent-contract>

## Intent

**Problem:** There is no leasing block machine, no transactional outbox, and no offline unlock redelivery, so CAP-6/7/8 cannot hold and a blocked car can still move.

**Approach:** Add a leasing-only machine (`REQUESTED`→`ARMED`→`SENT`→`ACKED`|`FAILED`|`TIMEOUT` plus `CANCELLED`), persist command+audit+outbox in one transaction, relay `leasing/{vin}/commands`, and keep a blocked or unlock-pending vehicle still. Prove with tests, logs, and `GET /api/commands/{id}`. No Carteira or policy POST.

## Boundaries & Constraints

**Always:**
- English states only. `ARMED` waits for speed 0, ignition off, and online — not the rental 5s window. `TIMEOUT` is `ARMED` offline more than 30s real. `REQUESTED`/`ARMED` cancel to `CANCELLED`. `SENT` cancel is refused (`in_transit`).
- Command, audit, and outbox rows commit in one transaction. A relay publishes the outbox payload to `leasing/{vin}/commands` and sets `sent_at`. Unlock-pending is a persisted unlock command, not a memory flag.
- A blocked (`ACKED` block) vehicle does not ignite or move. Unlock-pending stays still until the unlock is acked. `RefuseBlock` (~15%) on a reachable device yields `FAILED`.
- Ingest leasing telemetry feeds last-known for the machine (speed/ignition/online) without adding leasing VINs to the rental snapshot or `GET /api/vehicles`. Leasing acks drive the machine, not the rental 5s service.
- `GET /api/commands/{id}` returns a leasing record with the same JSON keys as rental (`id`, `vin`, `action`, `state`, `correlationId`). Bind stays `0.0.0.0:8300`.
- Consumer-owned ports. sqlc + goose for new queries. No `utils`. Rental `PENDING`/`SENT`/5s machine and `fleet/{vin}/` stay untouched.

**Never:**
- Carteira UI, policy POST (`/api/contracts/{id}/block|cancel|notify|payments`), rate limits, `?fleet=` SSE, or clock-speed control.
- Reuse rental command states or publish leasing on `fleet/{vin}/commands`.
- Advance `ARMED`→`SENT` from a timer. Mix leasing into `GET /api/vehicles`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Arm then send | Request block; telem speed 0, ignition off, online | `REQUESTED` then `ARMED`; next tick `SENT`; outbox published; device ack `ACKED` | Persist fail rolls back tx; no MQTT |
| Moving | `ARMED`; speed > 0 or ignition on | Stays `ARMED`; no outbox | None |
| Armed timeout | `ARMED`; no telem / offline > 30s | `TIMEOUT`; no MQTT sent | None |
| Cancel armed | Cancel while `REQUESTED` or `ARMED` | `CANCELLED`; vehicle not blocked | `SENT` cancel → in-transit error |
| Device refuse | `SENT`; `RefuseBlock` true | Ack ok=false → `FAILED`; vehicle not blocked | None |
| Immobilize | Block `ACKED` | Simulator does not set ignition or change lat/lng | None |
| Offline unlock | Unlock while blocked + offline | Persist unlock pending; no publish yet; on online telem, outbox publishes; ack → not blocked | Persist fail: stay blocked |
| No mix | Both fleets running | `GET /api/vehicles` still 20 rental; leasing command GET works by id | Unknown id → 404 |

</intent-contract>

## Code Map

Continuity: HEAD `f82f800`. Story 3 clock/book done. Story 2 `RefuseBlock` unused. Rental machine stays `internal/command`. Tables `commands`/`audit_log`/`outbox` exist empty.

- `migrations/00001_schema.sql:79-106` -- `commands`, `audit_log`, `outbox` already match architecture. No new migration unless a column is missing (do not add status columns to installments).
- `internal/store/queries/` -- add parameterized SQL for command/audit/outbox + leasing `vehicle_state` upsert. sqlc committed.
- `internal/store/postgres.go` / new `internal/store/block.go` -- `Begin`/`Commit` helper so command+audit+outbox share one tx. Do not write leasing into `Memory` snapshot.
- `internal/block/` -- new package: Request/Cancel/Unlock, Tick (ARMED conditions + 30s), ApplyAck, Get. Ports for store, last telem, live-stream (default live). No HTTP types. Not `utils`.
- `internal/outbox/` (or `block.Relay`) -- poll unsent outbox, publish via a consumer-owned `Publish(ctx, topic, payload)`, mark `sent_at`.
- `internal/ingest/ingest.go:91-163` -- leasing telemetry applies last-known into the block/telem port; leasing ack calls `block.ApplyAck`, not `command.Service.Apply`.
- `internal/sim/sim.go` `ackCommand` / `stepVehicle` -- leasing `block`/`unlock` actions: `RefuseBlock` on block; set/clear blocked; skip motion and ignition when blocked or unlock-pending.
- `internal/httpapi/httpapi.go:45-48,281` -- `GET /api/commands/{id}` already exists. Add a consumer lookup (or fan-in) so leasing ids resolve without changing rental JSON keys.
- `cmd/server/main.go` -- start block ticker + outbox relay; wire ingest; keep `pg.Seed(NewFleet())` rental-only. Shutdown: drain SSE, stop sim, flush/stop relay (architecture order).
- `internal/command/command.go` -- read-only this story.
- `{project-root}/openapi.yaml` -- document that `/api/commands/{id}` also returns leasing states/actions (`block`/`unlock`).

## Tasks & Acceptance

**Execution:**
- `internal/block/` -- machine + Tick + persist-via-store -- CAP-6/7/8
- `internal/store/queries/` + `internal/store/block.go` -- tx command/audit/outbox, leasing telem upsert -- parameterized SQL
- `internal/outbox/` or relay in block -- publish `leasing/{vin}/commands`, mark sent -- CAP-8 path
- `internal/ingest/ingest.go` -- leasing telem/ack into the machine -- no rental mix
- `internal/sim/sim.go` -- immobilize + RefuseBlock on block + unlock ack -- CAP-7
- `internal/httpapi/httpapi.go` -- GET command lookup includes leasing -- curl surface
- `cmd/server/main.go` -- wire ticker, relay, ingest, shutdown flush -- process
- `openapi.yaml` -- leasing command states on GET -- contract
- `internal/block/*_test.go`, `internal/sim/*_test.go`, `internal/httpapi/*_test.go` -- I/O matrix

**Acceptance Criteria:**
- Given a leasing VIN and a block request, when last telem is moving or ignition on, then the command is `ARMED` and no MQTT payload is published.
- Given that command and telem speed 0, ignition off, online, when Tick runs, then state is `SENT` and `leasing/{vin}/commands` is published from the outbox.
- Given `ARMED` and no online telem for more than 30s, when Tick runs, then state is `TIMEOUT` and the vehicle is not blocked.
- Given `SENT` and a device ack with ok=false (`RefuseBlock`), when the ack is applied, then state is `FAILED` and the vehicle may still move.
- Given `ACKED` block, when the simulator steps, then ignition stays off and lat/lng do not change.
- Given a blocked VIN that is offline, when Unlock is called, then an unlock command is persisted without publish; when telem is online, the relay publishes it; when acked, the vehicle may move again.
- Given `GET /api/vehicles` after these paths, when the snapshot is read, then it still has exactly 20 rental vehicles.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 38 findings — high 0, medium 16, low 8, false 12, maybe-false 2
- findings:
  - `[medium]` `[defer]` Simulator does not copy Load blocked flags onto NewLeasingFleet — restart motion needs a new Run hook
  - `[low]` `[reject]` Vehicle.unlockPending is never set true at runtime — leftover blocked already keeps the car still until unlock ack
  - `[low]` `[reject]` Load does not hydrate telem — ARMED timeout uses UpdatedAt; empty telem is offline by design
  - `[medium]` `[patch]` Request while blocked/in-flight accepted — now refused with ErrInTransit
  - `[medium]` `[patch]` Cancel vs Tick SENT race — Cancel marks CANCELLED in memory first and reverts on persist fail
  - `[false]` `[reject]` ApplyAck persist fail advances memory — persist error returns before replace
  - `[medium]` `[patch]` Flush lists at most 64 rows — now pages until empty
  - `[low]` `[reject]` SENT leasing has no ack timeout — spec TIMEOUT is ARMED offline 30s only
  - `[low]` `[reject]` Offline unlock has no TIMEOUT — pending unlock waits for telem by design
  - `[low]` `[reject]` Get treats store error as missing — Unlocker.Get is (Record, bool); 404 is the existing contract
  - `[low]` `[reject]` NoteTelem/ApplyAck persist on the ingest callback — same 2s write-through as rental Apply
  - `[medium]` `[patch]` RefuseBlock test does not observe motion — now asserts usable ignition/speed and lat/lng change
  - `[medium]` `[patch]` Load never tested — TestService_Load hydrates in-flight and ACKED blocked
  - `[medium]` `[patch]` Postgres test skipped audit/update/MarkSent — insert, update, MarkSent, lists, audit, UpsertTelem now asserted
  - `[medium]` `[patch]` Cancel persists then replace (edge) — same in-memory-first Cancel as above
  - `[medium]` `[patch]` Request while VIN blocked (edge) — same ErrInTransit refuse
  - `[medium]` `[patch]` FAILED later block clears blocked — ApplyAck only sets blocked on ACKED
  - `[medium]` `[patch]` Second Unlock while pending — refused with ErrInTransit
  - `[low]` `[reject]` Get store error 404 (edge) — same bool lookup contract
  - `[low]` `[reject]` tickUnlock ignores Live() — Live gates ARMED→SENT only; degraded SSE is story 5/7
  - `[medium]` `[patch]` Flush 64 (edge) — same paging loop
  - `[medium]` `[defer]` Load restores blocked but sim flags stay false (edge) — same restart hook as the first defer
  - `[false]` `[reject]` stepVehicle only honors flags from ackCommand — production ackCommand sets blocked; restart is the deferred hook
  - `[medium]` `[patch]` Load never restores in tests (gap) — TestService_Load added
  - `[medium]` `[patch]` Persist updates never hit PostgreSQL — Insert=false now asserted
  - `[medium]` `[patch]` MarkSent never hit PostgreSQL — MarkSent then ListUnsent empty
  - `[medium]` `[patch]` RefuseBlock test gap — same motion asserts
  - `[medium]` `[patch]` Persist-fail never reaches send — Tick after failPersist stays unsent
  - `[medium]` `[patch]` HasVIN never asserted — unknown VIN returns ErrUnknownVIN
  - `[medium]` `[patch]` GET vehicles no-mix never runs UpsertTelem — Snapshot stays rental-only after leasing telem
  - `[medium]` `[patch]` Unlock ack never asserts motion — unlock restores ignition/speed; test steps after ack
  - `[low]` `[reject]` unlockPending only set in a test literal — leftover blocked is the offline-still path
  - `[medium]` `[patch]` Unlock left ignition off — successful leasing unlock restores ignition and speed
  - `[false]` `[reject]` Intent L1 requires Carteira labels — starting intent is the machine, not chrome
  - `[false]` `[reject]` CAP-6/8 operator copy missing — POST/UI are story 5/7
  - `[false]` `[reject]` Immobilization must be one cross-package loop in tests — package APIs plus wiring is the L2 reading
  - `[false]` `[reject]` GET/logs not machine-integrated in tests — GET fan-in and slog are wired; story 5 owns write HTTP
  - `[maybe-false]` `[reject]` OpenAPI enums unread by a test — contract file is enough this story; would need a YAML assertion to settle

## Auto Run Result

Status: done

Summary: Leasing block machine (`REQUESTED`→`ARMED`→`SENT`→`ACKED`|`FAILED`|`TIMEOUT` plus `CANCELLED`) with transactional command+audit+outbox, relay on `leasing/{vin}/commands`, offline unlock redelivery, and simulator immobilization. `GET /api/commands/{id}` fans in leasing records. Review patches: refuse in-flight Request/Unlock, keep ACKED blocked on later FAILED, Cancel-before-Tick, paging Flush, restore motion after unlock, and tests for Load, HasVIN, send persist-fail, Postgres update/MarkSent/audit/telem, and motion after refuse/unlock.

Files changed:
- `internal/block/` — machine, Tick, Load, persist-via-store
- `internal/store/block.go`, `internal/store/queries/block.sql` — tx + telem upsert
- `internal/outbox/` — relay publish and sent_at
- `internal/ingest/ingest.go` — leasing telem/ack ports
- `internal/sim/sim.go` — RefuseBlock, blocked still, unlock motion
- `internal/httpapi/httpapi.go` — leasing command GET
- `cmd/server/main.go` — ticker, relay, shutdown flush
- `openapi.yaml` — leasing actions/states
- tests — matrix plus review coverage

Review: 16 patches applied (medium). 2 deferred (sim restart hydration). Rejected: unused unlockPending flag, Load telem, SENT/unlock timeouts, Get bool 404, ingest persist-on-callback, Live on unlock, and CAP-6/8 UI / cross-package-loop alignment notes.

Follow-up review recommended: true. Unverified residual: after process restart the simulator roster is built unblocked even when Load restored ACKED flags.

Verification:
- `gofmt -l` empty on touched packages
- `go vet` clean on touched packages
- `go test -count=1` on block/outbox/sim/store/httpapi/ingest/command/cmd/server pass
- `go test -race -count=1` on block/sim pass

Residual risks: policy POST, visitor audit hash, `?fleet=` SSE, and Carteira UI are later stories. Outbox publish is at-least-once if MarkSent fails after MQTT.

## Design Notes

Request is a package API this story (story 5 wraps it in POST + policy). `REQUESTED`→`ARMED` is the first Tick after accept, not a 5s sleep.

Online means a recent leasing telemetry apply (injectable clock in tests). Offline for TIMEOUT is the absence of that signal for 30s while `ARMED`.

Unlock-pending: `action=unlock`, not-yet-sent outbox (or sent_at null until online). Vehicle stays still until ack.

Audit origin this story is `SISTEMA` (no visitor hash yet). Correlation id is on the command row and logs.

A `Live() bool` port may gate `ARMED`→`SENT`; default true. Degraded SSE is story 5/7.

## Verification

**Commands:**
- `go test ./...` -- expected: block, outbox, sim, ingest, httpapi, command (rental) pass
- `go test -race ./internal/block/ ./internal/sim/` -- expected: no races on Tick/relay
- `gofmt -l` on touched packages -- expected: empty
- `go vet` on touched packages -- expected: clean
