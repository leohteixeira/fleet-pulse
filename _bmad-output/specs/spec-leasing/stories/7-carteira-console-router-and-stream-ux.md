---
title: 'Carteira console, router, and stream UX'
type: 'feature'
created: '2026-09-21'
status: 'done'
review_loop_iteration: 1
followup_review_recommended: false
baseline_revision: 'ace9c445aed4cc9f789552294eba3e896ed9d422'
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/ux.md'
  - '{project-root}/_bmad-output/specs/spec-leasing/architecture.md'
  - '{project-root}/docs/leasing/design/README.md'
warnings: []
deferred: []
---

<intent-contract>

## Intent

**Problem:** The leasing HTTP surface exists, but the evaluator still has only the Frota console. CAP-1/2/3/11/12 need a second console that looks like the adopted design.

**Approach:** Add `react-router-dom` (`/`, `/carteira`, `/carteira/:id`) and ship the Carteira console against `ux.md` plus the adopted HTML. Map 42%, panel 380px, markers 56×44 anchor [28,12]. One EventSource, `?fleet=` swap is not STREAM CAIU. 409/422/429 use designed Portuguese copy. Policy stays server-owned.

## Boundaries & Constraints

**Always:**
- Routes: `/` Frota (existing console, unchanged KPIs/map/panel). `/carteira` Carteira empty panel. `/carteira/:id` selected contract. Unknown id: Carteira mounted, empty panel, no toast.
- Header tabs: Frota / Carteira as designed. Carteira wordmark `SP · cobrança`. Simulated-date dashed chip from `GET /api/clock` (display only). Theme toggle and stream pill stay shared.
- Layout tokens: header 44px, KPI 56px, filter 40px, `--panel-w: 380px`, `--map-share: 42%`. Min 1280×800.
- Markers 56×44 `divIcon`, anchor [28,12], same car SVG as Frota. Chip `L17 23d`. ARMED = 2.4s dashed ring (`fp-armed`), never a spinner.
- Actions 2×2: Notificar, Solicitar bloqueio (dialog only), Cancelar bloqueio, Registrar pagamento. Refused buttons stay visible at opacity `.45`.
- Block only through the 480px dialog; confirm `Confirmar solicitação de bloqueio`; reason ≥ 8 trimmed chars.
- Writes send `Idempotency-Key`. Map server `code` to designed copy. Do not evaluate 16+/48h/online in the browser as source of truth.
- One EventSource, StrictMode-safe. Tab change closes and opens `?fleet=rental|leasing` without incrementing retries or painting the amber band.
- Portuguese UI copy. English identifiers/comments. pnpm only. No `utils`. No new Go policy.

**Never:**
- Mix rental VINs into Carteira map/table/MQTT queries.
- Hide a refused action.
- Clock-speed control in the UI.
- Treat intentional `?fleet=` swap as STREAM CAIU.
- npm/yarn/bun. Story 8 Compose/Caddy/retention.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Tabs | Click Carteira / Frota | Route changes; one EventSource swaps fleet; live pill stays | Swap must not increment retries |
| Select | Row or marker | `/carteira/{id}`, 380px panel | Unknown id → empty panel, no toast |
| Deselect | Close or map background | `/carteira`, empty crosshair | None |
| Filters empty | No row matches | Dashed empty + `Limpar filtros` | None |
| Notify 422 | Server `in_day` / `notify_too_recent` | Button .45 + designed policy line | No toast |
| Block | Dialog confirm | POST 202; command line ARMADO | Short reason keeps confirm .45 |
| 409 | `contract_busy` | Interval copy `{n}s` on all four | First write kept |
| 429 | rate limit | Red band `Limite de requisições…` | All four disabled |
| Stream drop | SSE error on current fleet | STREAM CAIU n/8 + freeze | Never red |
| Deep link | `/carteira/unknown` | Carteira + empty panel | No toast |

</intent-contract>

## Code Map

Continuity: HEAD `ace9c44`. Frota lives in `web/src/App.tsx`. Stream already uses `?fleet=rental`. Backend contracts/clock/leasing vehicles/writes exist.

- `web/package.json` -- add `react-router-dom` with `pnpm` (lockfile). AGENTS.md “no router” is superseded for this story.
- `web/src/main.tsx` -- `BrowserRouter`.
- `web/src/App.tsx` -- split Frota vs routes; keep Frota behavior.
- `web/src/Carteira.tsx` (and small presentational files) -- KPI, filters, map+table 42%, 380px panel, dialog, actions.
- `web/src/stream.ts` -- shared connect helper: `fleet`, `controlled` reconnect flag so tab swap is not degraded.
- `web/src/styles.css` -- Carteira tokens from ux.md / adopted HTML light+dark.
- `web/src/*carteira*.test.ts` -- routes, filter empty, unknown id, fleet swap not STREAM CAIU, 409/422/429 copy.
- Existing `MapView.tsx` / marker helpers -- reuse car SVG; leasing markers follow ux.md chip/ring rules. Do not put leasing VINs on the Frota map.
- `GET /api/clock`, `/api/contracts`, `/api/contracts/{id}`, `/api/leasing/vehicles`, write POSTs — consume as-is.

## Tasks & Acceptance

**Execution:**
- Router + shared header/stream -- CAP-1, CAP-11, CAP-12
- Carteira map/table/KPI/filters/panel -- CAP-2, CAP-3
- Actions + dialog + designed 409/422/429 -- CAP-4..10 surface (server already done)
- Tests -- I/O matrix

**Acceptance Criteria:**
- Given `/`, when the Carteira tab is clicked, then the location is `/carteira` and the EventSource URL contains `fleet=leasing` without a STREAM CAIU flash.
- Given `/carteira`, when a contract row is selected, then the location is `/carteira/{id}` and the panel is 380px.
- Given `/carteira/not-a-real-id`, when the page loads, then the empty panel is shown and no toast appears.
- Given a 422 `in_day` on notify, when the response arrives, then the notify button stays visible at .45 with `Contrato em dia`.
- Given a 429, when the response arrives, then the red rate-limit band appears and all four actions stay visible.
- Given an ARMED vehicle, then the marker uses the 2.4s dashed ring and no spinner.
- Given `/`, then rental KPIs and 20 rental markers still work.

## Spec Change Log

## Review Triage Log

- [accepted] medium: detail fetch depended on `selected` object identity and aborted every second. Now keyed on `selectedId` without abort-on-tick.
- [accepted] medium: 409 interval was global. Now scoped to the contract that received `contract_busy`.
- [accepted] medium: table/map pan+scroll fought each other. Selection sets one origin and children call onPanDone/onScrollDone.
- [accepted] medium: `.btn-secondary` used a hardcoded dark border; now `var(--line-strong)`.
- [accepted] low: reason length uses rune count (8–280), matching the server.
- [accepted] high (browser): `/carteira` and `/carteira/:id` remounted Carteira and flashed the empty panel. Single route `/carteira/:id?`.
- [deferred] verification: no Testing Library mount of Shell; browser pass covered tabs, select, unknown id, STREAM AO VIVO on swap.

## Design Notes

Pixel truth: `docs/leasing/design/Fleet Pulse Carteira.dc.html` and `ux.md`. Map share is **42%**, not the mock 52%.

Server policy codes already exist (`late_below_16`, `notify_required`, `notify_too_recent`, `device_offline`, `reason_invalid`, `in_day`, `notify_cooldown`, `payment_not_due`, `in_transit`, `contract_busy`). Map them to the ux.md Portuguese strings. Interval/rate use `retryAfter` / `Retry-After`.

Greater São Paulo viewport for Carteira (not rental Centro geofence). Esri Canvas World Gray.

## Auto Run Result

- `pnpm test` in `web/` — 37 tests pass
- `pnpm exec tsc --noEmit` — clean
- Browser: `/` Frota; `/carteira` 50 contracts + KPIs; row select → `/carteira/{id}` panel; `/carteira/not-a-real-id` empty panel no toast; tab swap keeps STREAM AO VIVO

Story 7 complete. Next: story 8 Compose, Caddy, embed, docs.
