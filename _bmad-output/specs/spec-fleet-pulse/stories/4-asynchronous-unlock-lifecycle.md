---
title: 'Asynchronous unlock lifecycle'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '7f0e995067cb5750123e247542866038fd0f67af'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/state-machines.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md'
warnings: []
deferred:
  - summary: >-
      architecture.md and state-machines.md still describe a one-deep 202 queue and PENDING timeout.
    evidence: |-
      Story 4 intentionally returns 409 for a second distinct unlock. Story 5 owns the queue and will realign those docs.
    location: >-
      _bmad-output/specs/spec-fleet-pulse/architecture.md
    severity: low
---

<intent-contract>

## Intent

**Problem:** CAP-3/CAP-8 have no HTTP or MQTT command path. An evaluator cannot unlock a vehicle or follow one correlation id from request to ack.

**Approach:** Add `POST /api/vehicles/{vin}/unlock` that creates a command, publishes on MQTT, and walks `PENDING` → `SENT` → `ACKED|FAILED|TIMEOUT`. Prove it with curl and logs; no panel.

## Boundaries & Constraints

**Always:**
- States `PENDING`, `SENT`, `ACKED`, `FAILED`, `TIMEOUT` only. Expiry is 5s from `SENT` (publish), not HTTP accept.
- `Idempotency-Key` scoped to vin+action `unlock`. Same key replays the existing record (same id, no restart).
- Response `202` JSON with command id (and current state). Missing key → `400`. Unknown VIN → `404`.
- One correlation id in slog JSON from the HTTP accept through MQTT publish to the ack (or timeout) line.
- Server publishes `fleet/{vin}/commands`; devices publish `fleet/{vin}/ack`. Real MQTT clients, not in-process calls.
- Online vehicles ack success. Offline VIN never acks → `TIMEOUT`. `FAILED` when an ack refuses (machine + tests; no 10% injection).
- `GET /api/commands/{id}` returns the record. SSE may emit `command` updates. Bind stays `0.0.0.0:8300`.
- Consumer-owned interfaces. No `utils` package. slog JSON. Packages by responsibility.

**Never:**
- Lock, one-deep queue, 10% refuse, panel, feed, geofence, theme, `go:embed`, `openapi.yaml`.
- Starting a second parallel machine: a distinct new unlock while one is `PENDING`/`SENT` returns `409` + the in-flight command (queue is story 5).
- UI buttons, Portuguese command copy, or changing marker derivation.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Unlock online | `POST .../unlock` + new `Idempotency-Key` on a reachable VIN | `202` `{id,state}`; publish; `SENT`; ack → `ACKED` | Publish error → stay/fail without a second id |
| Replay | Same VIN + action + key | `202` same `id` and state; no second publish | None |
| Timeout | Unlock `FPULSESAO00000020` | `SENT` then `TIMEOUT` after 5s from publish | No hang |
| Refuse ack | Ack payload marks refused | State `FAILED` | Bad ack JSON skipped; clock still runs |
| Distinct in-flight | Second new key while `PENDING`/`SENT` | `409` + in-flight command | None |
| Missing key / unknown VIN | No header / VIN not in store | `400` / `404` | None |

</intent-contract>

## Code Map

Continuity: HTTP + store + SSE exist (`7f0e995`). Broker has Subscribe only — no Publish. Sim publishes telemetry and ignores commands. Ingest is telemetry-only.

- `internal/httpapi/httpapi.go:45-51` -- add `POST /api/vehicles/{vin}/unlock` and `GET /api/commands/{id}`; declare a small `Unlocker` port
- `internal/store/store.go:49-54` -- extend Memory with command records (id, vin, action, state, key, correlation, sentAt) or keep them in `internal/command`
- `internal/broker/broker.go:92-111` -- add `Publish(ctx, topic, payload)` via inline client; keep mochi types inside
- `internal/ingest/ingest.go:48-72` -- also subscribe `fleet/+/ack`; parse commandId + accepted/refused; log correlation
- `internal/sim/sim.go:184-200` -- after connect, subscribe `fleet/{vin}/commands` and publish success ack (offline VIN still skipped)
- `cmd/server/main.go:74-79` -- wire command service, publisher, ack sink, expiry loop
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/state-machines.md` -- legal transitions
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md:47-63` -- topics and HTTP

## Tasks & Acceptance

**Execution:**
- `internal/command/command.go` -- create PENDING, publish → SENT, apply ack, expire at SENT+5s, replay by key -- machine lives here
- `internal/command/command_test.go` -- unit-test the I/O matrix (replay, timeout from SENT, refuse → FAILED, 409, missing key) -- lock the clock
- `internal/broker/broker.go` -- Publish without leaking mochi -- server can emit commands
- `internal/ingest/ingest.go` -- AckFilter + handler calling command Apply -- inbound acks stay an adapter
- `internal/sim/sim.go` -- subscribe commands, publish success ack -- real MQTT loop
- `internal/httpapi/httpapi.go` -- POST unlock 202 / GET command / 400 / 404 / 409 -- curl surface
- `cmd/server/main.go` -- wire publisher, acks, slog correlation -- one process path
- tests for HTTP + ingest ack + sim ack -- prove CAP-3/8 without a panel

**Acceptance Criteria:**
- Given a reachable VIN, when `POST /api/vehicles/{vin}/unlock` is sent with `Idempotency-Key`, then the status is `202` with a command id that becomes `ACKED`.
- Given the same key, when POST is repeated, then the body returns the same command id and no second command is created.
- Given `FPULSESAO00000020`, when unlock is posted, then the command is `TIMEOUT` 5s after `SENT`.
- Given the HTTP accept log and the ack (or timeout) log, when both are read, then they share one correlation id.
- Given an in-flight unlock, when a distinct key is posted, then the status is `409`.
- Given the tree, when `go test ./...` and `go test -race ./...` run, then both pass.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 27 findings — high 0, medium 10, low 10, false 7, maybe-false 0
- findings:
  - `[medium]` `[patch]` Publish fail leaves PENDING + inFlight — failPublish now marks FAILED and clears inFlight
  - `[medium]` `[patch]` HTTP 500 drops the created id — POST returns 202 {id,state} when rec.ID is set
  - `[low]` `[reject]` Ack topic VIN not checked — everyday acks carry the matching commandId
  - `[low]` `[reject]` Omitted ok becomes FAILED — sim always sends ok; not bad JSON
  - `[medium]` `[reject]` No in-repo HTTP→MQTT E2E test — live curl on this pass reached ACKED through real MQTT
  - `[low]` `[reject]` HTTP starts before sim subscribe — healthz + curl ACKED after vehicles connect
  - `[low]` `[defer]` architecture/state-machines still describe queue 202 — story 5 owns the queue
  - `[false]` `[reject]` httpapi imports command.Record — no user-facing defect
  - `[low]` `[reject]` sim command channel can block — one-deep unlock, ack succeeds
  - `[low]` `[reject]` SSE command event untested — Design Notes: GET is enough
  - `[medium]` `[patch]` RunExpiry untested — TestService_RunExpiry added
  - `[low]` `[reject]` PENDING→ACKED if ack wins the race — still a terminal; machine then skips SENT
  - `[medium]` `[patch]` Publish fail (edge) — same failPublish
  - `[medium]` `[patch]` HTTP 500 without id (edge) — same 202-with-id
  - `[low]` `[reject]` Missing ok → FAILED (edge) — same everyday payload
  - `[medium]` `[patch]` HTTP publish-error status untested (verification-gap) — 202 FAILED case added
  - `[medium]` `[patch]` Missing-key header not observed (verification-gap) — asserts unlocker.key == ""
  - `[medium]` `[patch]` TIMEOUT/RunExpiry untested (verification-gap) — same RunExpiry test
  - `[medium]` `[patch]` readySub(2) untested (verification-gap) — TestReadySub_SignalsAfterN
  - `[false]` `[reject]` CAP-3 UI walk missing (intent) — story 5 owns the panel
  - `[false]` `[reject]` Second in-flight should be 202 queue (intent) — story 4 contract is 409
  - `[false]` `[reject]` Through-MQTT not composed in tests (intent) — curl ACKED on this pass
  - `[false]` `[reject]` Correlation not logged in the HTTP handler (intent) — Unlock accept/publish/ack lines share the id
  - `[false]` `[reject]` 5s clock only stubbed (intent) — live offline VIN timed out at ~5s
  - `[false]` `[reject]` FAILED not injected via MQTT (intent) — 10% refuse is story 5; machine Apply(false) is tested
  - `[low]` `[reject]` Replay after publish fail does not republish (intent) — same id, no second command
  - `[low]` `[reject]` Client never sees PENDING (intent) — happy-path POST returns SENT

## Design Notes

Command JSON on MQTT can be `{id,action,correlationId}`. Ack JSON `{commandId,ok}` (false → FAILED). Generate both ids as strings. Use a clock interface so timeout tests do not sleep 5s. SSE `command` events are optional but useful for later panel work — GET is enough for this story's curl proof. Do not implement lock or the successor/queue slot.

## Verification

**Commands:**
- `go test ./...` -- expected: command, HTTP, ingest, sim, broker pass
- `go test -race ./...` -- expected: no races
- `go run ./cmd/server` plus curl -- expected: unlock online VIN reaches ACKED; replay same id; offline VIN TIMEOUT; logs share correlationId

## Auto Run Result

Status: done

Summary: `POST /api/vehicles/{vin}/unlock` creates an asynchronous command, publishes on MQTT, and walks PENDING → SENT → ACKED|FAILED|TIMEOUT. Same Idempotency-Key replays the id. One correlation id is logged from accept through ack or timeout.

Files changed:
- `internal/command` — machine, clock, failPublish, expiry
- `internal/httpapi` — POST unlock, GET command, 400/404/409/202
- `internal/broker` — Publish
- `internal/ingest` — fleet/+/ack
- `internal/sim` — subscribe commands, success ack
- `internal/store` — Has(vin)
- `cmd/server` — wire publisher, acks, RunExpiry, readySub(2)
- tests and story spec

Review findings: patched publish-fail → FAILED + 202 with id, missing-key header assert, RunExpiry, and readySub. Deferred architecture queue docs. Rejected the rest per the triage log.

Follow-up review: true. Four medium entries were patched (publish-fail handling, HTTP id, RunExpiry, readySub/missing-key tests). Unverified risk: failPublish + 202-on-error path was not re-read by the hunter layers.

Verification:
- `go test ./...` and `go test -race ./...` — pass
- curl: online VIN 202 SENT then ACKED; replay same id; missing key 400; unknown VIN 404; distinct in-flight 409; offline VIN TIMEOUT ~5s; logs share correlationId
