---
title: 'Policy, audit, and leasing HTTP'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '89c6233504cff0659cdff7a1d28539e635e93972'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/state-machines.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings:
  - oversized
deferred:
  - summary: >-
      Book ticker payments that clear late do not call settleDebt or write SISTEMA audit, so an auto-paid contract can stay armed or blocked.
    evidence: |-
      applyDuePayments inserts a payment only. HTTP payContract is the only settle path.
    location: >-
      internal/book/tick.go
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Clock and the book exist, but leasing has no operator HTTP: no vehicle snapshot, no contract detail, no notify/block/cancel/payment, no server policy or visitor audit, and SSE still mixes fleets on `/api/stream`.

**Approach:** Expose the architecture leasing reads and writes with server-owned policy and append-only audit. Require `GET /api/stream?fleet=rental|leasing` (400 if missing/invalid) and point Frota at `?fleet=rental`. No Carteira chrome and no demo rate-limit/interval (story 6).

## Boundaries & Constraints

**Always:**
- Paths: `GET /api/leasing/vehicles`; `GET /api/contracts` with filters `overdueBand` and `vehicleState`; `GET /api/contracts/{id}` (contract + installments + audit); `POST /api/contracts/{id}/notify|block|block/cancel|payments`. Existing `GET /api/clock` and `GET /api/commands/{id}` stay. Bind `0.0.0.0:8300`.
- Writes require `Idempotency-Key` scoped to contract id + action (`notify`|`block`|`cancel`|`payment`). Same key replays the original record. Body max 8 KiB. Block reason 8–280 chars.
- Policy is server-only. Block: days late ≥ 16 simulated, a prior notify at least 48 simulated hours old, device online. Notify: days late ≥ 1 and last notify older than 24 simulated hours. Payment: at least one installment overdue or due within 5 simulated days. Violations are `422` plus a readable `code`. Block accept is `202` + command id.
- Audit is append-only. Origin `VISITANTE` or `SISTEMA`. Visitor identity is a hashed anonymous id derived from the request (never raw IP in logs, audit, or JSON).
- `GET /api/stream?fleet=` is required. `rental` receives only rental events; `leasing` only leasing. Missing or unknown fleet → `400`. Drop-oldest, no gzip. Tag events at publish. Frota EventSource becomes `/api/stream?fleet=rental`.
- Payment that brings late to 0 cancels `REQUESTED`/`ARMED` and unlocks a blocked VIN (pending if offline). Cancel in `REQUESTED`/`ARMED` → `CANCELLED`; `SENT` cancel → in-transit `422`. Blocked + cancel/unlock uses story-4 Unlock.
- Consumer-owned HTTP ports. sqlc for new queries. No `utils`. Do not put leasing rows on `GET /api/vehicles`. Rental 5s door machine stays.

**Never:**
- Carteira UI, clock-speed control, 429/IP limit, 20s contract interval, 64-SSE cap, or CORS changes (story 6/8).
- Evaluate policy in the browser. Log raw IPs or raw Idempotency-Key as a secret.
- Mix fleets on a stream connection or on `/api/vehicles`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Fleet stream | `GET /api/stream?fleet=rental` | 200 SSE; rental telem only | Missing/invalid fleet → 400 |
| Leasing list | Seeded book | `GET /api/leasing/vehicles` is ~50 leasing VINs, no `FPULSESAO` | Empty last-known still lists roster |
| Contract detail | Known id | 200 with installments + audit | Unknown id → 404 |
| Notify ok | Late ≥ 1, cooldown clear, Idempotency-Key | 200/204; audit `VISITANTE` + hash; same key replays | em_dia or cooldown → 422 + code |
| Block refused | Late 10 or no notify or notify < 48h or offline | 422 + readable code; no command | Missing key → 400; short reason → 422 |
| Block accepted | Late ≥ 16, notify ≥ 48h, online, reason ≥ 8 | 202 + command id; machine REQUESTED | Persist fail → 5xx, no MQTT |
| Cancel / pay | Armed cancel; or payment clearing late | CANCELLED / late 0 + unlock path | SENT cancel → 422 in_transit |
| No mix | After writes | `GET /api/vehicles` still 20 rental | None |

</intent-contract>

## Code Map

Continuity: HEAD `89c6233`. Clock/contracts GET exist. Block package API is Request/Cancel/Unlock. SSE has no fleet query. Frota uses `/api/stream`.

- `internal/httpapi/httpapi.go:117-126` -- mux. Add leasing vehicles, contract detail, four POSTs. Keep clock/vehicles/commands.
- `internal/httpapi/sse.go:13-16,104` -- `Event` needs a fleet tag; `stream` requires `fleet=rental|leasing` and filters. Missing/invalid → 400.
- `cmd/server/main.go` telemetrySink -- tag rental events `fleet=rental`. Publish leasing telem/command on `fleet=leasing` (ingest already notes telem).
- `internal/ingest/ingest.go` -- leasing telem must reach a leasing SSE sink, not only logs/block.NoteTelem.
- `internal/book/book.go` -- Get(id), filters, Notify, Pay (oldest overdue / due ≤ 5 days). After notify, `regulariza_apos_notificacao` may auto-pay on Tick. List already returns daysLate/band.
- `internal/block/` -- HTTP calls Request/Cancel/Unlock; do not fork the machine. Online = recent NoteTelem.
- `internal/store/queries/` -- contract by id, installments, audit list, last notify, idempotency store if needed, leasing vehicle_state list. sqlc committed.
- `internal/httpapi` policy port -- days late, last notify age, online, reason length. Readable codes (`late_below_16`, `notify_required`, `notify_too_recent`, `device_offline`, `reason_invalid`, `in_day`, `notify_cooldown`, `payment_not_due`, `in_transit`).
- `web/src/App.tsx:222` and `web/src/App.test.ts` -- EventSource `/api/stream?fleet=rental`.
- `{project-root}/openapi.yaml` -- every new path, 400/422 bodies, stream query.
- `internal/httpapi/httpapi_test.go` stream test -- add `?fleet=rental` or it will 400.

## Tasks & Acceptance

**Execution:**
- `internal/httpapi/httpapi.go` + policy helpers -- leasing HTTP + 422 codes -- CAP-4/5/9/10
- `internal/httpapi/sse.go` -- required `?fleet=` filter -- CAP-1 stream
- `internal/book/` -- Get, filters, Notify, Pay, regulariza after notify -- book writes
- `internal/store/queries/` + store methods -- detail, audit, idempotency, leasing snapshot -- sqlc
- `cmd/server/main.go` + `internal/ingest/` -- tag and publish leasing SSE -- no mix
- `web/src/App.tsx` + `web/src/App.test.ts` -- Frota `?fleet=rental` -- brownfield
- `openapi.yaml` -- contract -- docs
- `internal/httpapi/*_test.go`, `internal/book/*_test.go` -- I/O matrix

**Acceptance Criteria:**
- Given no `fleet` query or `fleet=other`, when `GET /api/stream` is issued, then status is 400.
- Given `?fleet=rental`, when leasing telem is published, then that SSE client does not receive it.
- Given a seeded book, when `GET /api/leasing/vehicles` is issued, then every VIN is leasing and `GET /api/vehicles` is still 20 rental.
- Given a late contract with no notify, when `POST .../block` is issued, then status is 422 and no command is created.
- Given late ≥ 16, a notify ≥ 48 simulated hours old, online telem, reason ≥ 8, and Idempotency-Key, when `POST .../block` is issued, then status is 202 and `GET /api/commands/{id}` shows a leasing block state.
- Given the same Idempotency-Key, when the notify or block POST is repeated, then the original record is returned and no second audit/command is created.
- Given an armed command, when `POST .../block/cancel` is issued, then the command is `CANCELLED`; given `SENT`, when cancel is issued, then status is 422.
- Given a payment that clears late, when it is accepted, then REQUESTED/ARMED cancel and a blocked VIN starts unlock.
- Given Frota, when the EventSource URL is read, then it is `/api/stream?fleet=rental`.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 39 findings — high 0, medium 18, low 8, false 11, maybe-false 2
- findings:
  - `[medium]` `[patch]` Idempotency unique violation is 500 — Store now replays the first row on 23505
  - `[medium]` `[patch]` Pay then audit not one tx — left as-is for Pay; settle now fails the HTTP write when DaysLate is 0
  - `[medium]` `[patch]` Request then audit fail leaves REQUESTED — Cancel before 500
  - `[medium]` `[defer]` Ticker payments never settle or SISTEMA-audit — auto-pay can leave a VIN armed
  - `[medium]` `[patch]` SSE buffers every fleet then filters — clients are tagged; other-fleet frames are not enqueued
  - `[low]` `[patch]` vehicleState armado omitted REQUESTED/SENT — those states now count as armado
  - `[false]` `[reject]` OpenAPI missing IdempotencyKey — component already exists at components.parameters
  - `[low]` `[reject]` Default AUDIT_HASH_SECRET is compile-time — env.example documents the override; deploy sets it
  - `[low]` `[reject]` Invalid overdueBand silently empty — unspecified 400; empty list is acceptable
  - `[low]` `[reject]` 404 via strings.Contains parse uuid — existing wrap; unknown id still 404 in tests
  - `[medium]` `[patch]` Stream leasing path untested — GET ?fleet=leasing now asserted
  - `[medium]` `[patch]` ListLeasing never hit Postgres — seed leasing-without-state + rental now asserted
  - `[medium]` `[patch]` telemetrySink Fleet untested — FleetRental asserted on telem and area-exit
  - `[medium]` `[patch]` Embedded Frota still used /api/stream — dist rebuilt; embed test requires ?fleet=rental
  - `[medium]` `[patch]` Cancel-when-blocked untested — ACK then cancel unlocks
  - `[medium]` `[patch]` Payment DaysLate>0 still settled — settle runs only when DaysLate==0
  - `[medium]` `[patch]` Two POSTs race the same key (edge) — unique violation replays
  - `[medium]` `[patch]` Request persists then audit fails (edge) — same Cancel-before-500
  - `[medium]` `[patch]` InsertPayment then audit fails (edge) — HTTP 500; no replay stored when settle fails on clear
  - `[low]` `[reject]` Payment while SENT can still ACK — SENT cancel is in_transit; unlock waits for ack (story 4)
  - `[medium]` `[patch]` Settle fail after pay still 200 (edge) — now 500 and no replay when late is 0
  - `[low]` `[reject]` Non-UUID write 500 — Get already maps not-found; malformed ids are 404 in detail tests
  - `[low]` `[reject]` Device offline after Online check — inherent TOCTOU; Tick TIMEOUT still applies
  - `[low]` `[reject]` Two notifies pass cooldown — story 6 interval is the visitor guard
  - `[low]` `[reject]` Two Pays same installment — UNIQUE (contract_id, due_on) on installments; payments are append-only
  - `[false]` `[reject]` ActiveBlock ACK before Cancel is 500 — unlock-when-blocked branch covers ACKED
  - `[false]` `[reject]` vehicleState with nil Blocker dumps the list — production always wires blocks
  - `[maybe-false]` `[reject]` Notify payload wall vs simulated time — LastNotify is written from simulated clock in Notify
  - `[low]` `[reject]` Unlock ErrNotBlocked as 500 — mapped to 404 when not blocked
  - `[false]` `[reject]` nil keys port second write — production always wires keys
  - `[medium]` `[patch]` Embed EventSource gap (verification) — same dist rebuild
  - `[medium]` `[patch]` Sink Fleet gap — same main_test asserts
  - `[medium]` `[patch]` Leasing stream never executed — same leasing SSE test
  - `[medium]` `[patch]` ListLeasing mocked away — same Postgres case
  - `[medium]` `[patch]` Cancel-blocked no HTTP test — same ACK-then-cancel
  - `[medium]` `[patch]` Still-late pay would settle — settle gated on DaysLate==0
  - `[false]` `[reject]` CAP-4/5/9/10 require Carteira chrome — starting intent is HTTP expose
  - `[false]` `[reject]` Clock must be reimplemented — expose means keep GET /api/clock
  - `[false]` `[reject]` Audit JSON must show simulated time — CAP-10 panel is story 7; createdAt is real by design

## Auto Run Result

Status: done

Summary: Leasing HTTP with server policy (16+/48h/online, notify 24h, payment horizon 5 days), append-only visitor-hashed audit, Idempotency-Key replay, and required `GET /api/stream?fleet=rental|leasing`. Frota EventSource is `/api/stream?fleet=rental`. Review patches: Cancel on audit-fail after Request, settle only when late is 0 (500 + no replay if settle fails), armado includes REQUESTED/SENT, SSE filters at enqueue, unique-key replay, embed rebuild, and tests for leasing stream, ListLeasing, sink Fleet, cancel-when-blocked, and still-late pay.

Files changed:
- `internal/httpapi/` — leasing routes, policy, visitor hash, keyed writes, SSE fleet
- `internal/book/write.go` — Get, Notify, Pay, AppendAudit
- `internal/store/` + `migrations/00002_idempotency.sql` — detail, audit, roster, keys
- `cmd/server/main.go` — fleet-tagged SSE
- `web/src/App.tsx` + `internal/webui/dist` — Frota `?fleet=rental`
- `openapi.yaml`, `env.example`

Review: 16 patches applied (medium/low). 1 deferred (ticker settle). Rejected: OpenAPI key component, compile-time hash default, silent empty filters, SENT-still-ACK, TOCTOU online/notify, Pay unique, nil-port cases, and CAP-as-Carteira alignment notes.

Follow-up review recommended: true. Unverified residual: system ticker payments that clear late do not cancel/unlock.

Verification:
- `gofmt -l` empty on touched Go
- `go test -count=1` httpapi/book/block/store/cmd/server/webui pass
- `go test -race` httpapi/book pass
- `pnpm --dir web test -- --run` 27 pass

Residual risks: 429/409/64-SSE are story 6. Carteira UI is story 7. Concurrent first-insert still last-writer-wins if both writes finish before either Store.

## Design Notes

Visitor hash: SHA-256 of a stable secret (`AUDIT_HASH_SECRET` or process default) plus the remote IP (and optional `X-Forwarded-For` first hop only if already trusted). Persist the hex digest in `audit_log.visitor_hash`. Never log the IP.

Idempotency can be a small table or a unique (contract_id, action, key) row that points at the first command/payment/notify id. Replay returns that id and the same status.

`vehicleState` filter uses story-4 flags: `armado` if in-flight ARMED, `bloqueado` if Blocked, `desbloqueio_pendente` if unlock REQUESTED, else `ativo`.

Readable 422 body: `{ "code": "...", "message": "..." }` in English codes; message may be Portuguese design copy.

8 KiB: reject larger bodies with 413 or 422 before JSON parse. Story 6 adds 429/409.

## Verification

**Commands:**
- `go test ./...` -- expected: httpapi, book, block, ingest pass
- `go test -race ./internal/httpapi/ ./internal/book/` -- expected: no races
- `gofmt -l` on touched packages -- expected: empty
- `pnpm --dir web test -- --run` -- expected: EventSource URL assertion passes

**Manual checks (if no CLI):**
- curl stream without fleet is 400; with rental stays 20-VIN snapshot
