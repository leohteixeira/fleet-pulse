---
title: 'Simulated clock, contracts, and book lifecycle'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: 'e29910709e1dde24162fc8f6e8f1c018ae7ff19c'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings:
  - oversized
deferred: []
---

<intent-contract>

## Intent

**Problem:** There is no simulated calendar and no leasing book, so CAP-12 and the delinquency half of CAP-16 cannot be observed.

**Approach:** Add an environment-only simulated clock and persist a 40–60 contract book whose payer profiles (40/30/20/10) generate payments as the calendar advances. Expose `GET /api/clock` and a read-only `GET /api/contracts` so the mix is curl-visible. No Carteira UI.

## Boundaries & Constraints

**Always:**
- Calendar rate is environment-only: `1h/s`, `4h/s` (default, ×14400), `12h/s`. Invalid/missing → `4h/s`. Telemetry timestamps stay real time. No HTTP or UI control that changes the rate.
- `GET /api/clock` returns simulated date/time, the rate (and ×N), and real time. Bind stays `0.0.0.0:8300`.
- `GET /api/contracts` lists active leasing contracts with days late (against the simulated date), overdue band (`em_dia` | `1_15` | `16_30` | `acima_30`), payer profile, vin, client name. No rental VIN. No write routes (notify/block/pay) this story.
- Persist leasing vehicles (`fleet='leasing'`) plus customers, contracts, installments, and system payments. Do **not** put leasing rows into the rental snapshot cache or `GET /api/vehicles`.
- Profile weights at creation: 40% `pontual`, 30% `atrasa_ocasionalmente`, 20% `regulariza_apos_notificacao`, 10% `inadimplente`. Same `SIM_SEED` → same first book assignment in tests.
- Payments follow the profile at each due date as the calendar crosses it:
  - `pontual` pays on the due simulated day
  - `atrasa_ocasionalmente` pays on due day about half the time, otherwise 3–12 simulated days late
  - `regulariza_apos_notificacao` stays unpaid until a notify exists (none this story) — they remain late
  - `inadimplente` does not auto-pay
- Active book stays 40–60. Ending a contract and opening a new one is allowed. Keep ≥10% of **active** contracts in each overdue band (self-heal floor). Installment status is derived from the simulated date plus payments — do not store a redundant status column unless the schema already has one (it does not).
- Consumer-owned HTTP ports. sqlc + goose for new queries; no `utils`. Rental 5s machine, SSE drop-oldest, and story-2 MQTT prefixes stay.

**Never:**
- Carteira UI, clock speed control, notify/block/cancel/payment POST, outbox, block machine, or position history.
- Mix leasing into `GET /api/vehicles` or rental SSE.
- Call an external router. Change Centro polygon or rental roster size.
- Advance telemetry timestamps with the simulated clock.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Clock default | No calendar env | `GET /api/clock` is 200 JSON with rate `4h/s` (or ×14400) and a simulated date that advances ~4h per real second | None |
| Clock env | `CALENDAR_RATE=1h/s` or `12h/s` | Clock JSON reports that rate | Invalid value → default 4h/s, no crash |
| Book seed | Empty DB, `SIM_SEED` set | After boot, `GET /api/contracts` has 40–60 rows; each band has ≥10%; profiles appear with the 40/30/20/10 mix (± sampling on a 50-book) | Missing leasing VIN insert → fail boot with wrapped error |
| Advance dues | Clock crosses a `pontual` due date | A system payment appears; days late for that installment goes to 0 | Persist error logged; clock still advances |
| Inadimplente | Profile `inadimplente`, due date passed | Days late increases; no auto payment | None |
| No mix | Both fleets running | `GET /api/vehicles` still 20 rental; contracts only `FPULSELSG` (or other leasing prefix) | None |
| Floor | Band would drop under 10% | Lifecycle creates or keeps enough late/current contracts so each band is ≥10% of active | None |

</intent-contract>

## Code Map

Continuity: story 1 store is Postgres + rental cache (`e848035` era; HEAD `e299107`). Story 2 leasing MQTT is not in that cache and must stay out.

- `migrations/00001_schema.sql:37-77` -- `customers`, `contracts`, `installments`, `payments` already exist. `contracts.vin` FKs `vehicles`. Insert leasing `vehicles` rows before contracts.
- `internal/store/queries/vehicles.sql` -- rental-only insert. Add leasing vehicle insert (fleet='leasing') without changing `ListRentalVehicles`.
- `internal/store/postgres.go` -- rental Seed/Apply/Snapshot. Add a book/clock persistence API consumed by a new package; do not dump leasing into `Memory`.
- `internal/httpapi/httpapi.go:65-76` -- mux. Add `GET /api/clock` and `GET /api/contracts`. Declare consumer ports (`Clock`, `Contracts`) — store/clock packages implement them.
- `internal/httpapi/openapi_test.go` -- currently asserts six paths; update when openapi gains clock/contracts.
- `cmd/server/main.go` -- after `store.Open`, start the clock and seed/tick the book; wire HTTP ports. Keep `pg.Seed(NewFleet())` rental-only.
- `internal/sim/leasing.go` -- `NewLeasingFleet` / `FPULSELSG` VINs. Book seed should use the same VIN set so MQTT and contracts match.
- `{project-root}/openapi.yaml` -- add `/api/clock` and `/api/contracts`.
- `{project-root}/env.example` -- document `CALENDAR_RATE` (`1h/s` \| `4h/s` \| `12h/s`).
- No clock package yet — `internal/clock` (or `internal/book`) is the natural home. Not `utils`.

## Tasks & Acceptance

**Execution:**
- `internal/clock/` -- env rate, Now/simulated date, advance from real time; no HTTP types -- CAP-12
- `internal/book/` (or store methods) -- seed 40–60 contracts + customers + leasing vehicles + installments; tick payments and 10% floor / 40–60 lifecycle -- CAP-16 book
- `internal/store/queries/` -- sqlc for customers/contracts/installments/payments and leasing vehicle insert -- parameterized SQL
- `internal/httpapi/httpapi.go` -- `GET /api/clock`, `GET /api/contracts` -- curl surface
- `cmd/server/main.go` -- wire clock + book ticker + HTTP ports -- process
- `openapi.yaml` + `env.example` + `README.md` -- contract and rate env -- docs
- `internal/clock/*_test.go`, `internal/book/*_test.go`, `internal/httpapi/*_test.go` -- I/O matrix

**Acceptance Criteria:**
- Given no calendar env, when `GET /api/clock` is issued, then status is 200 and the body reports `4h/s` (or ×14400) plus a simulated timestamp and a real timestamp.
- Given `CALENDAR_RATE=12h/s`, when `GET /api/clock` is issued, then the reported rate is 12h/s (×43200).
- Given a fresh database and a running process, when `GET /api/contracts` is issued, then there are 40–60 active contracts, each band is ≥10% of that count, and no rental VIN appears.
- Given a `pontual` installment whose due date the simulated clock has crossed, when the book ticker runs, then a system payment is recorded and that installment is no longer late.
- Given an `inadimplente` installment past due, when the ticker runs, then days late is > 0 and no system payment is created for it.
- Given `GET /api/vehicles` after book seed, when the snapshot is read, then it still has exactly 20 rental vehicles.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 22 findings — high 0, medium 11, low 6, false 5, maybe-false 0
- findings:
  - `[medium]` `[patch]` Simulated elapsed × multiplier overflows int64 after a few wall days — elapsed is now clamped to MaxInt64/mult
  - `[medium]` `[patch]` Snapshot reads Simulated/Real/Rate under separate locks — one lock covers the whole snapshot
  - `[medium]` `[patch]` paymentBreaksFloor can refuse a pontual due-day payment — pontual (including due day) skips the floor check
  - `[medium]` `[patch]` List/Tick nil clock panics — List now returns `clock is required`
  - `[medium]` `[patch]` Clock HTTP does not assert simulated/real RFC3339 — fixture instants are compared
  - `[medium]` `[patch]` Contracts HTTP only checks VIN — id, clientName, payerProfile, daysLate, overdueBand asserted
  - `[medium]` `[patch]` atrasa_ocasionalmente has no Tick test — freeze day-before (0 payments) then advance to scheduled pay
  - `[medium]` `[patch]` healFloor/fitRange after calendar advance untested — 50-book after 16 sim days still ≥10% per band and 40–60
  - `[medium]` `[patch]` persist-error test does not observe book state — paymentCount stays 0 and installment unpaid
  - `[medium]` `[patch]` no Postgres Tick of InsertPayment — pontual due-today now records a system payment and daysLate == 0
  - `[medium]` `[defer]` payments.installment_id is nullable without UNIQUE — schema change belongs with later write routes
  - `[low]` `[defer]` addBandContract writes customer/contract/installments without a transaction — seed-path only; a later persist API can wrap
  - `[low]` `[defer]` no UNIQUE (vin) WHERE ended_on IS NULL — rotation is in-process this story
  - `[low]` `[reject]` endFromSafest map range is non-deterministic — floor/range still hold; same seed assignment is the specified invariant
  - `[low]` `[reject]` seed payments use time.Now not clk.Real — paid_at is display-only; days late uses the simulated date
  - `[low]` `[reject]` Seed shrinks n if leasing roster has fewer than 40 VINs — NewLeasingFleet is ~50; boot still fails on persist error
  - `[low]` `[reject]` httpapi imports clock.Snapshot and book.Contract — consumer ports stay in httpapi; adapters implement them
  - `[false]` `[reject]` CAP-12 requires the Carteira DATA SIMULADA chip — no Carteira UI this story
  - `[false]` `[reject]` GET /api/contracts is extra vs the story card — the intent contract requires the read-only list
  - `[false]` `[reject]` CAP-16 motion/block is missing — this story is the book half; motion was story 2
  - `[false]` `[reject]` notify/block/pay writes are missing — story 5
  - `[false]` `[reject]` telemetry timestamps follow the simulated clock — they stay real time by design

## Auto Run Result

Status: done

Summary: Environment-only simulated clock (`CALENDAR_RATE` 1h/s | 4h/s default | 12h/s) plus a persisted 50-contract leasing book (profiles 40/30/20/10, 10% overdue-band floor, 40–60 active). `GET /api/clock` and read-only `GET /api/contracts` are curl-visible. Leasing vehicles persist with `fleet='leasing'` and stay out of the rental snapshot. Review patches: clock overflow clamp and single-lock Snapshot, nil-clock List error, pontual bypass of the floor, and HTTP/book/Postgres Tick assertions.

Files changed:
- `internal/clock/` — env rate, origin 2026-01-01T00:00:00Z, Now/Snapshot
- `internal/book/` — seed, Tick (500ms), profiles, healFloor/fitRange
- `internal/store/book.go`, `internal/store/queries/book.sql` — customers/contracts/installments/payments
- `internal/httpapi/httpapi.go` — GET /api/clock and GET /api/contracts
- `cmd/server/main.go` — wire clock + book ticker
- `openapi.yaml`, `env.example`, `README.md`
- tests — clock HTTP, contracts JSON, atrasa tick, floor after 16 sim days, persist-error, Postgres pontual Tick

Review: 10 patches applied (medium). 3 deferred (payment UNIQUE, seed transaction, partial VIN unique). Rejected: map-range lifecycle, seed paid_at clock, roster shrink, HTTP adapter types, and CAP-12 UI / write-route / telemetry-clock alignment notes.

Follow-up review recommended: true. Unverified residual: payment rows can still duplicate without a UNIQUE; addBandContract is not transactional.

Verification:
- `gofmt -l` empty on touched packages
- `go test -count=1` on clock/book/httpapi/cmd/server pass
- `go test -race -count=1` on clock/book pass

Residual risks: notify/block/pay writes, outbox, and Carteira UI are later stories.

## Design Notes

Use `CALENDAR_RATE` (`1h/s` \| `4h/s` \| `12h/s`). Simulated origin can be a fixed date plus elapsed real time × rate so tests can inject a clock.

`regulariza_apos_notificacao` cannot auto-pay this story (no notify). They supply the late bands. The 10% floor may add or keep late contracts; it must not silently convert every `inadimplente` into `pontual`.

Tick the book from a loop on the process lifecycle context (e.g. every 250ms–1s real). Keep it deterministic under `SIM_SEED` + a fake clock in tests.

`GET /api/contracts` is read-only proof of the book. Story 5 adds filters, detail, and writes.

## Verification

**Commands:**
- `go test ./...` -- expected: clock, book, HTTP, store, sim tests pass
- `go test -race ./...` -- expected: no races on clock/book tick
- `gofmt -l .` -- expected: empty
- `go vet ./...` -- expected: clean

**Manual checks (if no CLI):**
- `curl /api/clock` shows 4h/s and a simulated date; `curl /api/contracts` is 40–60 with four bands; `curl /api/vehicles` still 20 rental
