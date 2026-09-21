---
title: 'Compose, Caddy, embed, and contract docs'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 1
followup_review_recommended: false
baseline_revision: 'b009170'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/brownfield.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** The demo still needs a one-binary + PostgreSQL + Caddy package so an evaluator can run it (CAP-15) with SPA embed, internal MQTT, healthchecks, and honest docs.

**Approach:** Extend Compose (`fleet-pulse`) with app + Caddy. Rebuild `go:embed` of the Vite build. Migrations on boot (already). MQTT not published. Retention for audit and finished commands. README names the trade-offs. openapi.yaml complete. Bind `0.0.0.0`.

## Boundaries & Constraints

**Always:**
- Compose project name `fleet-pulse`. Published ports stay on the Fleet Pulse assignment: web/Caddy **3300**, app HTTP **8300** only if needed internally — prefer Caddy as the public HTTP on 3300 proxying `/api` and `/healthz`. MQTT **must not** be published. Postgres stay on **5435** for local `go run` or internal-only in the demo stack.
- App image: migrations on boot, `0.0.0.0:8300`, slog JSON, graceful shutdown drains SSE first.
- `go:embed` serves the Vite build with `index.html` fallback for `/`, `/carteira`, `/carteira/:id`.
- Same-origin: Caddy serves UI and API on one host. CORS already same-origin.
- Retention job: bound `audit_log` and finished `commands` so disk stays stable. Position history is the first cut — do not add it.
- README documents DATABASE_URL, CALENDAR_RATE, SIM_SEED, trade-offs (no auth, in-memory telem cache, no position history).
- openapi.yaml covers clock, contracts, leasing vehicles, writes, stream `?fleet=`, 409/422/429.
- English docs. pnpm for the web build. No `utils`.

**Never:**
- Publish MQTT 1883 to the host in the public compose.
- Change rental 5s door machine.
- Add login. Add clock-speed UI.
- Cross-repo imports.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Compose up | `docker compose up -d` | postgres healthy, app healthy, Caddy serves `/` and `/carteira` | Missing env uses env.example defaults |
| SPA fallback | `GET /carteira/foo` via embed/Caddy | `index.html` | API 404 stays 404 |
| MQTT | scan published ports | 1883 not on host | Broker listens inside the network |
| Restart | kill app container | contracts still in Postgres | healthz 503 until ready |
| Retention | old audit / finished commands | deleted past retention window | persist fail logged |

</intent-contract>

## Code Map

HEAD `b009170`. `compose.yaml` is postgres-only. `internal/webui` embed exists from Frota — rebuild after `pnpm build`. `cmd/server` already requires DATABASE_URL and drains on shutdown.

- `compose.yaml` -- app + caddy + postgres; MQTT internal
- `Caddyfile` -- reverse proxy, SPA fallback, same origin
- `Dockerfile` -- multi-stage: pnpm build web, go build embed
- `internal/webui/` -- rebuild dist
- retention in `internal/store` or a small `internal/retain` ticked from main
- `README.md`, `env.example`, `openapi.yaml`

## Tasks & Acceptance

**Execution:**
- Compose/Caddy/Dockerfile -- CAP-15
- embed rebuild + SPA fallback -- Carteira routes
- retention job -- disk bound
- README + openapi -- contract

**Acceptance Criteria:**
- Given Compose, when services are healthy, then Caddy on 3300 serves Frota and `/carteira` and proxies `/api`/`/healthz`.
- Given the published port list, when 1883 is checked on the host, then it is not published.
- Given a Vite build embedded, when `GET /carteira` hits the app or Caddy, then the SPA HTML is returned.
- Given finished commands older than the retention window, when the job runs, then those rows are deleted.
- Given README, then it names no-auth, Postgres required, MQTT internal, and calendar env-only.

## Spec Change Log

## Review Triage Log

- [accepted] parent: compose publishes only 3300 and 5435; MQTT 1883 is not in `compose.yaml`.
- [accepted] parent: SPA fallback covered by httpapi tests; retention 7 days in store tests.
- [deferred] no full `docker compose up --build` in this pass; local Caddy is HTTP on 3300, not automatic public HTTPS.

## Design Notes

Workspace matrix: Fleet Pulse web 3300, API 8300, MQTT 1883. Public demo: Caddy:3300 is the visitor URL. App:8300 can stay internal. Postgres 5435 may stay published for `go run` or be internal-only if the app always uses the compose network.

Retention window: 7 days for finished commands and audit (SPEC: bound disk; position history cut).

## Auto Run Result

- `go test ./cmd/server/ ./internal/webui/` — pass
- Compose: project `fleet-pulse`, published 3300 + 5435, no host 1883
- Embed rebuilt (`index-DhbSEsYh.js`)

Story 8 complete. Spec-leasing stories 1–8 are done.
