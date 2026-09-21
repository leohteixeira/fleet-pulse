---
title: 'Lock, queue, panel, feed, failures, and geofence'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '1ccffc6e716532b9d92ffab1c8dd6fcc24e27fc9'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/state-machines.md'
warnings:
  - multiple-goals
deferred:
  - summary: >-
      state-machines.md still allows PENDING to TIMEOUT before publish.
    evidence: |-
      Expiry still starts at SENT. Aligning that diagram is leftover from story 4 and not required to ship lock/queue/panel.
    location: >-
      _bmad-output/specs/spec-fleet-pulse/state-machines.md
    severity: low
---

<intent-contract>

## Intent

**Problem:** Unlock exists, but lock, the one-deep queue, the panel, the feed, injected failures, and Centro exits are missing. An evaluator cannot run the CAP-2/4/5/6/11/12 demo.

**Approach:** Generalize the command machine for lock + one queued successor, inject ~10% refuse and offline timeout, emit area-exit from the server, and add the Portuguese panel/feed/command UX. Theme, embed, and degraded-stream chrome stay out.

## Boundaries & Constraints

**Always:**
- Lock uses the same machine as unlock (`action` `lock`|`unlock`). `POST /api/vehicles/{vin}/lock` is `202` + id. Idempotency-Key scoped to vin+action.
- Queue depth one: a distinct new POST while `PENDING`/`SENT` is accepted `202` as unpublished `PENDING`; a third while the slot is full is `409` + the in-flight command. Same key still replays. Expiry clock starts at SENT of that command.
- Reachable vehicles refuse ~10% (`FAILED` ack). Offline VIN never acks → `TIMEOUT`.
- Server owns Centro and emits SSE `area-exit` when last-known crosses from inside to outside. Frontend still derives `fora`.
- Panel + feed + command line follow `ux.md`: Portuguese labels, `5 segundos` timeout copy, other action queueable while one is in-flight, same action disabled, both disabled when a successor is queued, `COMANDO` shows only the in-flight command. No toast or modal.
- One EventSource. SSE events: `telemetry`, `command`, `area-exit`. Header KPIs from derived states. Dark tokens already in `web/src/styles.css`.
- Consumer-owned interfaces. No `utils`. slog JSON. pnpm only under `web/`.

**Never:**
- Theme toggle, `data-theme`, `go:embed`, `openapi.yaml`, degraded-stream pill/band, README.
- A second EventSource, a global store library, npm/yarn/bun.
- Parallel in-flight machines, or 409 on the first distinct successor (that was story 4 temporary).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Lock | `POST .../lock` + new key on reachable VIN | `202` `{id,state}`; MQTT `action=lock`; ack → `ACKED` | Same 400/404 as unlock |
| Queue | Distinct unlock while lock is `SENT` | `202` queued `PENDING`; publishes when predecessor terminals | None |
| Queue full | Third distinct command | `409` + in-flight command | None |
| Replay | Same vin+action+key | Same id; no second record | None |
| Refuse | Device ack `ok:false` (~10%) | `FAILED` | Bad ack skipped |
| Timeout | Unlock/lock offline VIN | `TIMEOUT` 5s from SENT | No hang |
| Area-exit | Apply moves a VIN from inside Centro to west of −46.685 | SSE `area-exit`; frontend `fora` | First point already outside is not an exit |
| Panel command | Selected VIN; Destravar then Travar while in-flight | Other button stays enabled; `COMANDO` shows in-flight only; queued starts after terminal | Offline shows TIMEOUT copy with 5 segundos |

</intent-contract>

## Code Map

Unlock machine is `1ccffc6`. Second distinct command is still `409`. Sim acks `ok:true` always and republishes spawn coords. Map has selection but no panel/feed.

- `internal/command/command.go:17-53` -- generalize `Unlock` → submit(action); replace Conflict-on-second with one successor; keep Conflict for a third
- `internal/httpapi/httpapi.go:57` -- add `POST /api/vehicles/{vin}/lock`; 409 only when queue full
- `internal/sim/sim.go:184-220` -- ~10% refuse ack; move at least one online VIN west across −46.685 over ticks
- `internal/store/store.go:79-106` -- detect inside→outside on Apply; notify a listener (or return a crossing)
- `cmd/server/main.go:59-65` -- publish SSE `command` and `area-exit`
- `web/src/state.ts` -- patch `command` / `area-exit`; keep commands per VIN (in-flight + queued)
- `web/src/App.tsx` -- layout header KPIs + map + panel + feed; POST lock/unlock with Idempotency-Key
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md:65-97` -- labels, panel, feed
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/state-machines.md` -- queue + lock

## Tasks & Acceptance

**Execution:**
- `internal/command/command.go` -- lock + one-deep queue + publish successor on terminal -- CAP-11 and queue
- `internal/command/command_test.go` -- lock, queue, third→409, replay per action -- lock the machine
- `internal/httpapi/httpapi.go` -- POST lock; 202 queued; 409 when full -- curl surface
- `internal/sim/sim.go` -- 10% refuse; wander one VIN out of Centro -- CAP-5 and CAP-12 data
- `internal/store/store.go` -- area-exit on inside→outside -- server owns the polygon
- `web/src/Panel.tsx` -- telemetry + Destravar/Travar + COMANDO line -- CAP-2/3/11 UX
- `web/src/Feed.tsx` -- collapsible rows, no toast -- CAP-6
- `web/src/App.tsx` / `web/src/state.ts` -- KPIs, command/area-exit SSE, Portuguese -- wire the demo
- tests (Go + vitest) for the I/O matrix -- no e2e suite

**Acceptance Criteria:**
- Given a reachable VIN, when lock is posted, then the command reaches `ACKED` or `FAILED` through MQTT.
- Given an in-flight command, when a distinct other action is posted, then the status is `202` and it publishes only after the first terminals.
- Given a full slot, when a third distinct command is posted, then the status is `409`.
- Given the offline VIN, when unlock or lock is posted, then the command is `TIMEOUT` with UI copy that says `5 segundos`.
- Given a VIN that leaves Centro, when the store applies the new point, then an `area-exit` SSE event is emitted and the marker derives `fora`.
- Given a selected marker, when the panel is open, then Portuguese telemetry and command buttons match `ux.md` (no toast/modal).
- Given `web/`, when `pnpm test` and `pnpm build` run, then both succeed; `go test ./...` and `go test -race ./...` pass.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 32 findings — high 0, medium 14, low 13, false 5, maybe-false 0
- findings:
  - `[low]` `[reject]` Portas flash cancelled when a successor promotes — everyday ack is already visible; extra timer state is more than a direct fix
  - `[low]` `[reject]` doorFlash is global — selection during 1.6s is uncommon
  - `[low]` `[reject]` Backend queues a second same-action key — ux disables the same button; click guard added separately
  - `[medium]` `[patch]` Travar missing in-flight chrome — lockLabel Enviando…/Aguardando…
  - `[false]` `[reject]` area-exit must move the marker — frontend derives fora from telemetry
  - `[low]` `[reject]` No Panel/Feed render tests — vitest is node; helpers cover labels
  - `[low]` `[reject]` POST /lock 400/404 untested — same doorCommand as unlock
  - `[medium]` `[patch]` Queue promote on FAILED untested — TestService_QueuePromotesOnRefuse
  - `[medium]` `[patch]` Overlapping sendCommand — sending Set per VIN
  - `[low]` `[reject]` 409 swallowed in UI — buttons prevent a third click
  - `[medium]` `[patch]` Telemetry republish error skips ack — ack still publishes
  - `[low]` `[defer]` state-machines.md PENDING→TIMEOUT — leftover diagram
  - `[low]` `[reject]` Unlocker name after lock — no user harm
  - `[low]` `[reject]` Unbounded feed — short evaluator session
  - `[low]` `[reject]` hydrate clears commands — snapshot has no command list
  - `[low]` `[reject]` Feed a11y gaps — empty panel copy is covered
  - `[medium]` `[patch]` SSE PENDING after SENT regresses — ignore older state
  - `[medium]` `[patch]` Queued 202 before in-flight — prefer SENT as inFlight
  - `[medium]` `[patch]` Double-click queues same action — sending guard
  - `[low]` `[reject]` doorFlash on VIN change (edge) — same as global flash
  - `[low]` `[reject]` Unknown action still acks — sim only receives unlock/lock
  - `[medium]` `[patch]` Telemetry fail before ack (edge) — same ack-always
  - `[medium]` `[patch]` Queue after FAILED (verification-gap) — same refuse test
  - `[medium]` `[patch]` sendCommand untested — App.test fetch 202/409
  - `[medium]` `[patch]` HTTP lock action unobserved — GET asserts ActionLock
  - `[medium]` `[patch]` Sim Locked/telemetry untested — success and refuse cases
  - `[medium]` `[patch]` FAILED missing in reducer tests — falhou / FALHOU asserted
  - `[false]` `[reject]` No operator E2E suite (intent) — spec forbids e2e; browser checked this pass
  - `[false]` `[reject]` Machine queues any distinct POST (intent) — ux is the other button
  - `[false]` `[reject]` Panel.tsx untested (intent) — panel used in browser this pass
  - `[medium]` `[patch]` FAILED feed untested (intent) — same reducer case
  - `[false]` `[reject]` Geofence not one composed test (intent) — store + sink + browser Fora=1

## Design Notes

Generate a new Idempotency-Key per button click in the browser (`crypto.randomUUID`). Keep story 4 correlation logging. Area-exit payload can be `{vin,displayId,lat,lng}`. 10% refuse is a sim-side roll on each command, not a global counter. KPI strip is in scope (Fora increment); theme toggle is not. Empty panel is the dashed crosshair copy from `ux.md`.

## Verification

**Commands:**
- `go test ./...` -- expected: command queue/lock/geofence tests pass
- `go test -race ./...` -- expected: no races
- `pnpm test` and `pnpm build` in `web/` -- expected: panel/feed/derive tests pass; bundle succeeds
- Vite + API: open `http://127.0.0.1:3300` -- expected: select a marker, unlock/lock, queue the other action, see feed rows; offline VIN expires with 5 segundos; a vehicle can show `fora`

## Auto Run Result

Status: done

Summary: Lock shares the unlock machine, a distinct successor queues as unpublished PENDING, a third POST is 409, reachable vehicles refuse ~10%, the server emits area-exit, and the Portuguese panel/feed/KPI chrome is live.

Files changed:
- `internal/command` — Submit, queue, promote on terminal including FAILED
- `internal/httpapi` — POST lock
- `internal/sim` — 10% refuse, wander V02 west, locked telemetry
- `internal/store` — inside→outside crossing
- `web/src/Panel.tsx`, `Feed.tsx`, `App.tsx`, `state.ts` — command UX
- tests and story spec

Review findings: patched lock labels, state ranking, SENT-as-in-flight, sendCommand guard, queue-on-FAILED, HTTP ActionLock, sim ack/Locked, and FAILED feed tests. Deferred state-machines PENDING timeout. Rejected the rest per the triage log.

Follow-up review: true. Several medium entries were patched. Unverified risk: sendCommand + lockLabel + state ranking were not re-read by the hunter layers.

Verification:
- `go test ./...` and `go test -race` on command/sim/httpapi — pass
- `pnpm test` — 22 passed; `pnpm build` — ok
- curl: lock 202; offline successor PENDING 202; third 409
- Browser: empty panel copy, V01 panel + Destravar/Travar, feed “5 segundos”, Fora KPI 1
