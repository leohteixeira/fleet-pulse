---
title: 'Theme, degraded stream, embed, and contract docs'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: false
baseline_revision: 'f4cd0b58c3e07042eb047556d9bbdc7b61eaa7f6'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/architecture-diagrams.md'
warnings:
  - multiple-goals
deferred:
  - summary: >-
      Ingest still cancels on the signal context when SIGINT arrives, so it
      stops in parallel with SSE drain rather than after it.
    evidence: |-
      cmd/server/main.go ingest.Run(ctx) uses the NotifyContext; only HTTP and
      sim have a later cancel. Architecture names drain → sim → broker.
    location: cmd/server/main.go
    severity: low
---

<intent-contract>

## Intent

**Problem:** The demo still needs the light theme, CAP-9 degraded-stream treatment, a single-process embedded UI, the six-endpoint contract, and a README that names the in-memory trade-off.

**Approach:** Add the theme toggle and amber reconnect chrome, `go:embed` the Vite build with `index.html` fallback, write `openapi.yaml`, and a README with a diagram plus trade-offs. Drain SSE clients first on shutdown.

## Boundaries & Constraints

**Always:**
- Theme: dark default; light is `:root[data-theme="light"]`; toggle in the header; persist `localStorage` key `fleetpulse-theme`. Light tiles `World_Light_Gray_Base`. Tokens from `ux.md`.
- Degraded stream is amber, never red: pill `STREAM CAIU · RECONECTANDO (n/8)`, 2px dashed amber band on the map, overlay `Posições congeladas · último dado há Ns`, markers freeze, unlock/lock wait until the stream is live again. One EventSource; reconnect on error.
- Embed the production UI (`pnpm build` in `web/`) via `go:embed`. Unknown paths fall back to `index.html`. Evaluator path is `go run ./cmd/server` on `:8300`.
- `openapi.yaml` documents the six HTTP endpoints: vehicles, stream, unlock, lock, command GET, healthz.
- README has an architecture diagram (may copy from `architecture-diagrams.md`) and a trade-offs section that names the in-memory store.
- Shutdown: `signal.NotifyContext`, drain SSE clients first, then stop the simulator, then close the broker.
- Portuguese UI copy. pnpm only under `web/`. No `utils` package.

**Never:**
- New command states, a second EventSource, npm/yarn/bun, a database, auth, or e2e tests.
- Red reconnect chrome, or treating a dropped stream as a hard failure.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Theme persist | Toggle to light, reload | `data-theme="light"` and `localStorage.fleetpulse-theme=light`; light tiles | Missing/invalid storage → dark |
| Stream drop | EventSource error | Amber pill with retry count; band + freeze overlay; `setLatLng` stops; commands disabled | Reconnect; never red |
| Stream live | Open / reconnect success | Green `STREAM · AO VIVO`; commands enabled | None |
| Embed | `GET /` on :8300 after embed | SPA HTML from the Vite build | Unknown path → `index.html` |
| OpenAPI | `openapi.yaml` | Six paths listed with methods | None |
| Shutdown | SIGINT | SSE clients closed first, then sim, then broker | Join close errors |

</intent-contract>

## Code Map

Story 5 left dark-only UI, one EventSource with no error chrome, API-only HTTP (no static files), no README/openapi. Shutdown waits on the same ctx for HTTP+sim then closes the broker — SSE is not drained first.

- `web/src/App.tsx:80-90` -- header: add theme toggle + stream pill
- `web/src/App.tsx:55-60` -- EventSource: onerror reconnect, expose live/retry to the shell
- `web/src/MapView.tsx` -- light tile URL; amber top band + freeze overlay; skip `setLatLng` when frozen
- `web/src/styles.css` -- `:root[data-theme="light"]` tokens from `ux.md`
- `internal/httpapi/httpapi.go:67-102` -- drain hub on shutdown; serve embedded files + fallback
- `cmd/server/main.go:99-117` -- stop HTTP/SSE first, then sim, then broker
- `internal/webui/` -- create; `go:embed` the built dist
- `{project-root}/openapi.yaml` -- create; six endpoints
- `{project-root}/README.md` -- create; diagram + in-memory trade-off
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md:17-46` -- light tokens and stream copy

## Tasks & Acceptance

**Execution:**
- `web/src/theme.ts` -- read/write `fleetpulse-theme`; apply `data-theme` -- persist light
- `web/src/App.tsx` -- toggle + stream pill; reconnect EventSource; block commands while down -- CAP-9
- `web/src/MapView.tsx` -- light tiles, amber band, freeze markers -- CAP-9 chrome
- `web/src/styles.css` -- light tokens -- ux.md
- `internal/webui/` -- embed `dist` after `pnpm build` -- CAP-7
- `internal/httpapi/httpapi.go` -- static + `index.html` fallback; drain SSE first -- one URL
- `cmd/server/main.go` -- ordered shutdown -- architecture
- `openapi.yaml` -- six endpoints -- CAP-10
- `README.md` -- diagram + in-memory trade-off -- CAP-10
- tests for theme, reconnect/freeze, fallback, drain order -- lock the I/O matrix

**Acceptance Criteria:**
- Given the header toggle, when light is selected and the page reloads, then the document stays light and tiles switch to World Light Gray.
- Given an EventSource error, when the stream drops, then the chrome is amber (never red), markers freeze, and command buttons wait.
- Given `go run ./cmd/server`, when `GET http://127.0.0.1:8300/` is issued, then the embedded SPA HTML is returned.
- Given `openapi.yaml`, when it is read, then it lists the six HTTP endpoints.
- Given the README, when it is read, then it includes a diagram and names the in-memory store trade-off.
- Given SIGINT, when the process exits, then SSE clients are drained before the simulator and broker stop.
- Given `web/`, when `pnpm test` and `pnpm build` run, then both succeed; `go test ./...` and `go test -race ./...` pass.

## Spec Change Log

- 2026-09-21 — implemented theme, CAP-9 chrome, embed, OpenAPI, README, ordered shutdown.

## Review Triage Log

- patched: map-wrap now fills the body grid so Leaflet keeps a height after the freeze overlay wrapper.
- patched: stream helpers live in `stream.ts` so Panel does not import App.
- rejected: rewriting native EventSource into a manual reconnect loop — one instance plus `error`/`open` matches CAP-9.
- deferred: ingest still shares the signal context (low).

## Design Notes

Copy the mermaid runtime diagram into the README (or a rendered equivalent). Build `web/` then copy `web/dist` into the embed package so `go run ./cmd/server` works without Vite. EventSource reconnect can close and open a new instance on the same effect, incrementing n up to 8. Stream clock can reuse the 1s `now` ticker. Do not add auth, a database, or an e2e suite.

## Verification

**Commands:**
- `pnpm test` and `pnpm build` in `web/` -- expected: theme/stream tests pass; dist built
- `go test ./...` and `go test -race ./...` -- expected: embed fallback + drain tests pass
- `go run ./cmd/server` -- expected: `curl -sS http://127.0.0.1:8300/` is the SPA; `/healthz` 200
- Browser `http://127.0.0.1:3300` or `:8300` -- expected: theme toggle persists; killing the API shows amber reconnect chrome

## Auto Run Result

Status: done

Summary: Light theme persists, dropped streams stay amber and freeze the map, `go run ./cmd/server` serves the embedded SPA, OpenAPI lists the six endpoints, the README names the in-memory trade-off, and shutdown drains SSE before the simulator and broker.

Files changed:
- `web/src/theme.ts`, `stream.ts`, `App.tsx`, `MapView.tsx`, `Panel.tsx`, `styles.css` — theme + CAP-9 chrome
- `internal/webui` — `go:embed` of the Vite build
- `internal/httpapi` — SPA fallback + Hub.Drain
- `cmd/server` — drain → HTTP → sim → broker
- `openapi.yaml`, `README.md`, story spec
- tests for theme, reconnect, fallback, drain order

Review findings: patched map-wrap height and the App/Panel import cycle. Deferred ingest-on-signal. Rejected a second EventSource.

Follow-up review: false.

Verification:
- `pnpm test` — 27 passed; `pnpm build` — ok
- `go test ./...` and `go test -race` — pass
- curl `:8300/` SPA HTML; `/healthz` 200; unknown path = index; `/api/missing` 404
- Browser `:3300` — light persist + World Light Gray tiles; kill API → amber `RECONECTANDO (n/8)`, freeze overlay, `rgb(180, 83, 9)`
- Browser `:8300` — 20 markers, `STREAM · AO VIVO`, dark default
