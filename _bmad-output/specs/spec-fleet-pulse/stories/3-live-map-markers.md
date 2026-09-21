---
title: 'Live map markers'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 0
followup_review_recommended: true
baseline_revision: '3048493489e0243791e96d7a7e0f915324b9b664'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md'
  - '{project-root}/docs/design/README.md'
warnings: []
deferred:
  - summary: >-
      AGENTS.md and CLAUDE.md still describe the repository as greenfield without a frontend.
    evidence: |-
      Both files still say there is no frontend and leave pnpm scripts for when the frontend exists. This story added web/. Fixing this edits agent-context files.
    location: >-
      AGENTS.md
    severity: medium
  - summary: >-
      Workspace portfolio-bootstrap does not restore web/ pnpm dependencies.
    evidence: |-
      Bootstrap installs a root package.json only. The frontend lives at web/package.json. Updating the workspace script or README is outside this story.
    location: >-
      web/package.json
    severity: medium
---

<intent-contract>

## Intent

**Problem:** CAP-1 data exists as JSON and SSE, but nothing renders the fleet. An evaluator cannot see vehicles or derived states on a map.

**Approach:** Add a Vite React 19 app that paints the snapshot on an Esri Canvas World Gray map and keeps 44×44 markers live from one EventSource. Derive disponível, em uso, fora da área, and offline on the client. Functional live markers first; polish, panel, theme toggle, and commands stay out.

## Boundaries & Constraints

**Always:**
- `web/` is a pnpm + Vite + React 19 + TypeScript app. react-leaflet v5, or imperative Leaflet in a `useRef` if the peer range fails. Plain CSS. No router, no state library, no component library.
- Fleet state is `useReducer` indexed by VIN. One `EventSource` to `/api/stream`, StrictMode-safe (connect once; abort on real unmount).
- First paint from `GET /api/vehicles` (polygon + vehicles). SSE `telemetry` events patch by VIN.
- Marker: 44×44 `divIcon`, car SVG 14×24 from `Fleet Pulse Spec.dc.html`, chip `displayId`, anchor `[22,12]`. Rebuild icon only when presentation state, selection, or heading rounded to 10° changes; otherwise `setLatLng` only. No CSS position transition.
- Derive states on the client only: `offline` if no telemetry yet or last telemetry older than 60s; else `fora` if point is outside the snapshot polygon; else `disponível` if parked + locked + ignition off; else `em uso`. Portuguese chips. Dark default tokens from `ux.md`.
- Vite binds `0.0.0.0:3300` and proxies `/api` and `/healthz` to `http://127.0.0.1:8300`.
- Tiles: Esri `World_Dark_Gray_Base`, attribution `© Esri, HERE, OpenStreetMap`.

**Never:**
- Unlock/lock, command panel, event feed, theme toggle, `go:embed`, `openapi.yaml`, or backend presentation labels.
- A second EventSource, a global store library, or rebuilding the icon on every lat/lng tick.
- npm/yarn/bun — `pnpm` only, with `pnpm-lock.yaml` under `web/`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| First paint | Snapshot with 20 vehicles + Centro polygon | 20 markers appear; offline VIN is `offline`; others derive from seed fields + no-telemetry rule | Snapshot fetch failure shows a Portuguese error, no markers |
| Telemetry patch | SSE `telemetry` for an online VIN | Marker `setLatLng` (and icon rebuild only if state/heading-bucket/selection changed) | Bad JSON skipped; reducer unchanged |
| Fora | Last point outside Centro | Presentation state `fora` (amber) | None |
| Heading bucket | Heading 14° then 16° | Icon not rebuilt (both bucket 10°); 14° then 25° rebuilds | None |
| StrictMode | Dev double mount | Still one live EventSource | Previous connection closed |
| Stream drop | EventSource error | Markers keep last position (freeze); no second connection until the single reconnect path | Reconnect is allowed; chrome/pill is story 6 |

</intent-contract>

## Code Map

Continuity: API already serves snapshot + SSE (`11c20e5`). No `web/` yet. Sim still republishes spawn coords — markers must still apply patches.

- `internal/httpapi/httpapi.go:45-51` -- `GET /api/vehicles`, `GET /api/stream`; Vite proxies here
- `internal/store/store.go:10-16` -- Centro bounds the frontend uses from the snapshot, not a hardcoded copy
- `{project-root}/_bmad-output/specs/spec-fleet-pulse/ux.md:48-61` -- marker anatomy and state fills
- `{project-root}/docs/design/Fleet Pulse Spec.dc.html` -- car path `M6 .5C9 .5…` and glass path
- `{project-root}/docs/design/Fleet Pulse.dc.html:254-264` -- divIcon html + `iconSize [44,44]`, `iconAnchor [22,12]`
- `{project-root}/AGENTS.md:36-40` -- React 19, Vite, react-leaflet v5, useReducer by VIN, one EventSource
- `web/package.json` -- create; pnpm scripts `dev`, `build`, `test`
- `web/vite.config.ts` -- create; host `0.0.0.0`, port 3300, proxy `/api` and `/healthz`
- `web/src/state.ts` -- create; reducer by VIN, derive presentation state, heading bucket
- `web/src/MapView.tsx` -- create; map + markers; rebuild icon only on bucketed identity
- `web/src/App.tsx` -- create; fetch snapshot, one EventSource, Portuguese shell
- `web/src/state.test.ts` -- create; I/O matrix for derive + heading bucket + patch

## Tasks & Acceptance

**Execution:**
- `web/package.json` -- React 19, TypeScript, Vite, react-leaflet v5 (or leaflet + react), vitest; `pnpm-lock.yaml` -- install with `pnpm install` inside `web/`
- `web/vite.config.ts` -- bind `0.0.0.0:3300`; proxy `/api` and `/healthz` to `127.0.0.1:8300` -- browser talks same-origin
- `web/src/state.ts` -- `useReducer` actions `hydrate`/`patch`; `deriveState`; `headingBucket` (10°) -- client owns presentation
- `web/src/state.test.ts` -- unit-test the I/O matrix (offline, fora, disponível, em uso, heading bucket, bad patch) -- lock derivation
- `web/src/MapView.tsx` -- Esri dark gray tiles; 44×44 divIcon; `setLatLng` vs rebuild; click selects -- functional markers
- `web/src/App.tsx` -- load snapshot, one StrictMode-safe EventSource, Portuguese title -- wire the live path
- `web/src/styles.css` -- dark tokens from `ux.md`, marker chip, no position transition -- enough for live, not theme toggle

**Acceptance Criteria:**
- Given the API is up, when the operator opens `http://127.0.0.1:3300`, then the map shows 20 markers without a page refresh being needed for later patches.
- Given an SSE telemetry event, when it arrives, then that VIN's marker position updates via `setLatLng` unless state, selection, or heading-bucket changed.
- Given the silent VIN, when the snapshot hydrates, then its marker derives `offline`.
- Given a last point west of −46.685 or south of −23.585 (outside Centro), when state is derived, then the state is `fora`.
- Given React StrictMode, when the app mounts, then only one EventSource stays open.
- Given `web/`, when `pnpm test` and `pnpm build` run, then both succeed; `go test ./...` still passes.

## Spec Change Log

## Review Triage Log

### 2026-09-21 — Review pass
- verdicts: 28 findings — high 0, medium 11, low 11, false 6, maybe-false 0
- findings:
  - `[false]` `[reject]` Hydrate zeros lastSeen so first paint is all offline — Design Notes require no-telemetry-since-hydrate as offline; snapshot has no lastSeen
  - `[medium]` `[patch]` No 60s clock — added 1s setInterval on `now`
  - `[low]` `[patch]` EventSource opens before hydrate — connectTelemetry runs only after `state.polygon`
  - `[low]` `[reject]` parsePatch skips type checks — everyday sim publishes numeric EncodeTelemetry
  - `[false]` `[reject]` Missing lat/lng becomes (0,0) — API snapshot always sends coordinates
  - `[false]` `[reject]` Vite proxy may buffer SSE — browser showed 19 `em_uso` via `/api/stream` proxy
  - `[medium]` `[defer]` AGENTS.md/CLAUDE.md still say no frontend — agent-context files
  - `[medium]` `[defer]` portfolio-bootstrap skips `web/` — workspace script / story 6 docs
  - `[medium]` `[patch]` App/SSE path untested — added `App.test.ts` for fetch + EventSource
  - `[low]` `[reject]` Fora tests only west/south — I/O matrix names those two edges
  - `[low]` `[patch]` Esri tiles empty past zoom 16 — TileLayer `maxZoom={16}`
  - `[low]` `[reject]` Snapshot fetch has no timeout — local API; adding retry is extra
  - `[low]` `[reject]` headingBucket skips 360/negative — sim headings stay in range
  - `[low]` `[reject]` tsconfig.node.json not referenced — vite.config is not user-facing
  - `[false]` `[reject]` Map never fitBounds — zoom 14 shows all 20 markers in the Centro view
  - `[medium]` `[patch]` 60s clock (edge) — same interval
  - `[low]` `[patch]` SSE before hydrate (edge) — same connect-after-polygon
  - `[low]` `[reject]` Patch non-number fields (edge) — same everyday payload
  - `[false]` `[reject]` Snapshot non-number lat/lng (edge) — API always sends numbers
  - `[low]` `[reject]` displayId omitted from icon identity — display ids are stable seed chips
  - `[medium]` `[patch]` EventSource telemetry untested (verification-gap) — `connectTelemetry` + fake EventSource
  - `[medium]` `[patch]` setLatLng vs setIcon untested (verification-gap) — `planMarkerUpdate` unit tests
  - `[medium]` `[patch]` Snapshot fetch/error untested (verification-gap) — `loadSnapshot` tests
  - `[medium]` `[patch]` Map/EventSource surface untested (intent) — same App tests
  - `[medium]` `[patch]` Snapshot+SSE path untested (intent) — same App tests
  - `[false]` `[reject]` First paint all offline (intent) — same Design Notes rule
  - `[low]` `[reject]` Label `fora` vs “fora da área” — chip shows displayId; state is color
  - `[medium]` `[patch]` Tests only covered state.ts (intent) — App + planMarkerUpdate tests added

## Design Notes

Copy the spec HTML car path and glass path verbatim. Treat "no telemetry since hydrate" as offline so the silent VIN is offline on first paint. `em uso` is the residual non-parked state (speed>0 or ignition on or unlocked). Do not add the header KPI strip, feed, or panel beyond a selected chip/ring — story 5 owns those. If react-leaflet v5 cannot install against React 19, fall back to Leaflet in a `useRef` and say so in the PR notes of the implementer report.

## Verification

**Commands:**
- `pnpm test` (in `web/`) -- expected: derive/heading tests pass
- `pnpm build` (in `web/`) -- expected: production bundle succeeds
- `go test ./...` -- expected: backend unchanged and green
- Vite + API: open `http://127.0.0.1:3300` -- expected: 20 live markers, offline VIN styled offline, SSE patches applied

## Auto Run Result

Status: done

Summary: Vite React 19 app paints the Centro snapshot on Esri World Dark Gray and keeps 44×44 markers live from one EventSource. Presentation states are derived on the client; the silent VIN stays offline.

Files changed:
- `web/` — pnpm Vite React 19 + react-leaflet v5 app, proxy to :8300
- `web/src/state.ts` — reducer, deriveState, heading bucket, planMarkerUpdate
- `web/src/MapView.tsx` — tiles, Centro rectangle, setLatLng vs rebuild
- `web/src/App.tsx` — snapshot fetch, 1s clock, EventSource after hydrate
- tests and story spec

Review findings: patched 60s clock, EventSource-after-hydrate, maxZoom 16, and tests for loadSnapshot/connectTelemetry/planMarkerUpdate. Deferred AGENTS.md and workspace bootstrap. Rejected the rest per the triage log.

Follow-up review: true. Four medium entries were patched (clock + three verification-gap tests). Unverified risk: the new `connectTelemetry` after-hydrate path and `App.test.ts` were not re-read by the hunter layers.

Verification:
- `pnpm test` (web/) — 13 passed
- `pnpm build` (web/) — production bundle ok (Leaflet chunk size warning)
- `go test ./...` — pass
- Browser `http://127.0.0.1:3300`: 20 markers, V20 offline, 19 em_uso after SSE, V07 selection ring

