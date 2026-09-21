---
title: 'Public demo protection and self-heal'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 1
followup_review_recommended: false
baseline_revision: '2de1f2056ff3a6714dec76a42036ed8d0026e49d'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** Visitors can hammer writes, stack SSE clients, and leave cars blocked forever, so a public demo book can empty or freeze (CAP-13).

**Approach:** Enforce server-owned guards — 6 writes/60s/IP (`429` + `Retry-After`), 20s per contract (`409` + readable code), 64 SSE connections, 30 simulated-day max block, and keep the 10% overdue-band floor after visitor actions. Prove with HTTP tests/curl. No Carteira chrome.

## Boundaries & Constraints

**Always:**
- Write routes (notify/block/cancel/payments) count per visible client IP: 6 per rolling 60s → `429` with `Retry-After` (seconds until a slot frees). Idempotent replay of the same key does not consume a new slot.
- A second mutating action on the same contract inside 20s real → `409` plus readable `code` (e.g. `contract_busy`) and a seconds-remaining hint. Replay of the same Idempotency-Key is not a second action.
- At most 64 simultaneous SSE connections process-wide. The 65th `GET /api/stream?fleet=` is `429` (or `503`) with a readable refusal; existing clients stay.
- Self-heal on the book ticker: each overdue band stays ≥10% of active; any VIN blocked longer than 30 simulated days is unlocked (pending if offline). Visitor Pay/Notify must not disable healFloor.
- Same-origin CORS on write routes if not already present. 8 KiB body limit already exists — keep it. Bind stays `0.0.0.0:8300`.
- Guards are server-owned. Do not hide a 409/429 in later UI this story. Consumer-owned ports. No `utils`.

**Never:**
- Carteira UI, clock-speed control, or changing rental 5s door limits.
- Trust the browser for interval/rate/floor. Log raw IPs.
- Mix leasing into `GET /api/vehicles`. Drop the 10% floor or the 40–60 book cap.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Rate limit | 7th distinct write from same IP in 60s | 429 + Retry-After; no 7th side effect | Replay of same key still 200/202 |
| Contract interval | Second action on same id inside 20s | 409 + code; first write kept | Same key replay allowed |
| SSE cap | 65th stream client | Refused; first 64 stay subscribed | Missing fleet still 400 first |
| Max block | VIN ACKED blocked > 30 sim days | Ticker unlocks (or pending if offline) | Persist fail logged; next tick retries |
| Floor after pay | Visitor pays so a band would drop under 10% | healFloor restores the band; book stays 40–60 | Persist fail logged |
| No mix | After guards | `GET /api/vehicles` still 20 rental | None |

</intent-contract>

## Code Map

Continuity: HEAD `2de1f20`. Guards sit in `leasingWrite` after replay and before `fn`. Book ticker heals the floor and unlocks stale blocks.

- `internal/httpapi/guards.go` -- in-memory 6/60s IP rate and 20s contract reserve/release.
- `internal/httpapi/cors.go` -- same-origin CORS (`Origin` host+port+scheme must match `Host`).
- `internal/httpapi/leasing.go` -- `leasingWrite` applies interval then rate; replay skips both; 2xx keeps the reserve.
- `internal/httpapi/sse.go` -- `Subscribe` returns `errStreamFull` at 64; fleet 400 happens first.
- `internal/book/tick.go` -- `healFloor` repeats until stable; `unlockCleared` + `unlockStale` after payments.
- `internal/block/block.go` -- `UnlockStale` / `UnlockIfBlocked`; `ackSimulated` uses calendar × rate with overflow clamp.
- `internal/clock/clock.go` -- `Multiplier()` for the 30-day conversion.
- `cmd/server/main.go` -- `Book.SetUnblocker(block.Service)`.
- `web/vite.config.ts` -- proxy strips `Origin` so local Vite writes are not 403.
- `openapi.yaml` -- 409 `contract_busy`, 429 `Retry-After`, SSE `stream_full`.

## Tasks & Acceptance

**Execution:**
- `internal/httpapi/` -- IP rate 6/60s, contract 20s, CORS, 64 SSE -- CAP-13 guards
- `internal/book/tick.go` + block port -- 30-day unblock + keep healFloor after writes -- self-heal
- `openapi.yaml` -- 409/429/SSE cap -- contract
- tests -- I/O matrix

**Acceptance Criteria:**
- Given 6 distinct writes from one IP in 60s, when a 7th distinct write is issued, then status is 429 and Retry-After is a positive integer.
- Given a successful write on contract C, when another action on C is issued 1s later with a new Idempotency-Key, then status is 409.
- Given 64 SSE clients, when a 65th connects with a valid fleet, then it is refused and the original 64 still receive events.
- Given an ACKED block older than 30 simulated days, when the book ticker runs, then the VIN is no longer Blocked (or unlock is pending).
- Given a visitor payment that would drop a band under 10%, when the ticker runs, then that band is ≥10% and active count is 40–60.
- Given `GET /api/vehicles` after these paths, then it still has exactly 20 rental vehicles.

## Spec Change Log

## Review Triage Log

- [accepted] high: reserve the 20s contract slot in the same lock as the check so concurrent POSTs cannot both mutate. `reserveInterval` / `releaseInterval`; 4xx restores the previous timestamp.
- [accepted] medium: CORS must match scheme+host+port. Dropped `stripHostPort` fallback. Vite proxy removes `Origin` so local same-origin `/api` writes still reach the API.
- [accepted] medium: `UnlockStale` is a no-op without a calendar; `ackSimulated` clamps `elapsed*mult` like `clock.simulatedAt` and treats zero `UpdatedAt` as already old.
- [accepted] medium: `healFloor` re-walks bands after `n` grows so a visitor pay cannot leave a band under the ceil-10% floor.
- [accepted] verification: 20s window with injectable clock; concurrent reserve; 7th write has no side effect; missing `fleet` before occupying SSE slots; 64th client still receives; 8 KiB accepted / 9 KiB 413; 29 sim-day block stays; healFloor measures the drop then the restore.
- [deferred] medium: rate slot is recorded before `fn`. A unique-violation replay after `Store` fails still consumed the attempt. Rare; replay of the same key still short-circuits before guards.

## Design Notes

Rate and interval are real-time, not simulated. Max block is simulated days: `ackSim = Simulated() - (Real()-UpdatedAt)*Multiplier` (default ×14400). 181 real seconds at 4h/s is just over 30 sim days.

Identify the client with the visible IP (`stripHostPort`; `X-Forwarded-For` only if `TRUST_FORWARDED_FOR`). Counters stay in memory. Never log raw IPs.

409 body: `{ "code": "contract_busy", "message": "...", "retryAfter": N }`. 429 also sets the `Retry-After` header. SSE 65th is `{ "code": "stream_full" }`.

Interval is reserved before `fn` and released on 4xx so a double-click cannot land two writes. Rate counts distinct new attempts (including 422), not replays.

SSE cap is process-wide across both fleets. CORS is exact Origin↔Host; curl without Origin is allowed.

Ticker payments that clear `DaysLate==0` call `UnlockIfBlocked`. `healFloor` still runs after visitor Pay/Notify.

## Auto Run Result

- `go test -count=1 ./internal/httpapi/ ./internal/book/ ./internal/block/` — pass
- `go test -race -count=1 ./internal/httpapi/ ./internal/book/` — pass, no races
- `gofmt` on touched Go files — clean

Story 6 complete. Next: story 7 Carteira console / router / stream UX.

## Verification

**Commands:**
- `go test ./internal/httpapi/ ./internal/book/ ./internal/block/` -- expected: pass
- `go test -race ./internal/httpapi/ ./internal/book/` -- expected: no races
- `gofmt -l` on touched packages -- expected: empty
