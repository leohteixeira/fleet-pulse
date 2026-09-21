---
id: SPEC-leasing
companions:
  - architecture.md
  - architecture-diagrams.md
  - state-machines.md
  - ux.md
  - brownfield.md
  - ../../../docs/leasing/design/Fleet Pulse Carteira Spec.dc.html
  - ../../../docs/leasing/design/Fleet Pulse Carteira.dc.html
sources:
  - ../../../docs/leasing/brief.md
  - ../../../docs/leasing/design/README.md
---

> **Canonical contract.** This SPEC and the files in `companions:` are the complete, preservation-validated contract for what to build, test, and validate. Source documents listed in frontmatter are for traceability — consult them only if you need narrative rationale or prose color this contract intentionally omits.

# Fleet Pulse phase 2 — Leasing and persistence

## Why

A **pain to solve** and a **vision to realize**. Phase 1 showed an honest door-command lifecycle on a rental fleet. Phase 2 exists because remote immobilization of a financed car is unsafe if treated as a synchronous call: it must wait for a stopped, ignition-off vehicle, leave a prior notice, and keep an audit trail. A public, login-free demo that visitors can hammer must also recover by itself. The narrative user is a collections operator at a leasing book; the real user is a visitor or interviewer on one URL. Inspiration is a telemetry immobilizer that lowers recovery risk and the same capability offered to banks as SaaS — this ship is still a portfolio artifact, not that product.

## Capabilities

- **CAP-1**
  - **intent:** An operator can switch between the rental console and the leasing portfolio without mixing fleets.
  - **success:** Routes `/` (Frota) and `/carteira` (Carteira) via react-router; header tabs match the design. Carteira map, table, queries, and logical MQTT topics contain only financed vehicles; rental VINs never appear there.

- **CAP-2**
  - **intent:** An operator can see the live leasing fleet on the designed map with the designed KPIs.
  - **success:** About 50 financed vehicles move from incoming telemetry without a page refresh. Markers are the designed 56×44 `divIcon` with contract-id plus days-late chips. The KPI row shows Contratos ativos, Valor total em atraso, Bloqueios armados, Veículos bloqueados.

- **CAP-3**
  - **intent:** An operator can filter the book and inspect one contract’s installments and audit.
  - **success:** Band chips, vehicle-state filter, and plate/client search work together. Default sort is days late descending, then client. Selecting a row or marker opens the 380px panel and goes to `/carteira/{id}`. Clearing selection returns the designed empty crosshair and `/carteira`. Unknown `{id}` stays on Carteira with the empty panel and no toast. Empty filters show the designed dashed empty state plus `Limpar filtros`.

- **CAP-4**
  - **intent:** An operator can notify a late client.
  - **success:** Accepted when late ≥ 1 simulated day and the last notification is older than 24 simulated hours. Audit records `VISITANTE · Notificação enviada`. The button hint shows `última DD/MM/YYYY` or `nenhuma notificação enviada`. In-day contracts refuse with `Contrato em dia`.

- **CAP-5**
  - **intent:** An operator can request a remote block only through the designed dialog and only when policy holds.
  - **success:** The 480px dialog requires a reason of at least 8 characters. Confirm is `Confirmar solicitação de bloqueio`. `POST` returns `202` with a command id. Policy violations return `422` with a readable error code and the designed copy: `16+ dias`, prior notification, notification ≥ 48h, device online.

- **CAP-6**
  - **intent:** An operator can watch a block wait for a safe vehicle condition before it is sent.
  - **success:** UI walks `SOLICITADO` → `ARMADO · aguardando veículo parar` while the vehicle is moving or the ignition is on. `SENT` happens only at speed 0, ignition off, and device online. Cancel stays available in `ARMED`. `SENT` refuses cancel with `Comando em trânsito`.

- **CAP-7**
  - **intent:** An operator can see a confirmed block immobilize the car on Carteira.
  - **success:** On `ACKED` the vehicle becomes `Bloqueado`, motion reads `IMOBILIZADO`, and the marker uses the filled red chip. The simulator will not turn ignition on or move that vehicle.

- **CAP-8**
  - **intent:** An operator can cancel an armed block or release a blocked vehicle, including when the device is offline.
  - **success:** Cancel in `REQUESTED` or `ARMED` yields `CANCELLED` and `Ativo` (neutral, not an error). Release on a blocked vehicle sends immediately if online, or stays `desbloqueio_pendente` and is delivered on reconnect.

- **CAP-9**
  - **intent:** An operator can register a payment that clears debt and releases the vehicle.
  - **success:** A payment advances the oldest overdue installment. When late becomes 0, `REQUESTED`/`ARMED` cancel and a blocked vehicle unlocks (pending if offline). Refused when the contract is fully paid or has no installment overdue or due within 5 simulated days.

- **CAP-10**
  - **intent:** An operator can read an append-only audit trail with real and simulated time.
  - **success:** The panel lists origin `VISITANTE` or `SISTEMA`, action, and reason. Visitor identity is a hashed anonymous id derived from IP — never the raw IP. A new row slides in 4px. No toast.

- **CAP-11**
  - **intent:** An operator can tell when the live stream is degraded and that positions are frozen.
  - **success:** On SSE drop: amber `STREAM CAIU · RECONECTANDO (n/8)`, 2px dashed amber band at the top of the map, `Posições congeladas`. Markers freeze. `ARMED` does not advance until the stream is live. Nothing turns red. Switching `?fleet=` on a tab change is a controlled reconnect and must not use this degraded treatment.

- **CAP-12**
  - **intent:** An evaluator can read a simulated calendar that advances independently of telemetry time.
  - **success:** The header shows the designed dashed `DATA SIMULADA` chip with a speed badge (default `×14400`, 4h per real second). Speed is environment-only; the UI does not expose `1h/s` / `4h/s` / `12h/s`. `GET /api/clock` exposes the simulated date and rate. The stream clock stays real time.

- **CAP-13**
  - **intent:** A public visitor can use every action, and the demo stays usable when they abuse it.
  - **success:** Writes at 6 per 60s per IP return `429` plus `Retry-After` and the designed red band. A second action on the same contract inside 20s returns `409` with a readable code and the designed countdown. Writes require `Idempotency-Key`. Self-heal keeps ≥10% of the active book in each overdue band and unblocks any vehicle blocked longer than **30 simulated days**.

- **CAP-14**
  - **intent:** An evaluator can restart the process without losing the portfolio.
  - **success:** After a container restart, contracts, commands, and audit are still in PostgreSQL. The phase-1 in-memory implementation is gone; the consumer-owned store interface remains.

- **CAP-15**
  - **intent:** An evaluator can run the public demo as one binary plus PostgreSQL and Caddy.
  - **success:** Compose starts app, PostgreSQL, and Caddy with automatic HTTPS. Migrations run on boot. MQTT is reachable only on the internal network. App and database healthchecks pass.

- **CAP-16**
  - **intent:** Both fleets can live continuously without a perceptible loop, and the book can move through delinquency on its own.
  - **success:** After one hour, trajectories and events do not perceptibly repeat. Production never calls an external router. Contracts enter and leave arrears from payer profiles weighted 40% pontual / 30% atrasa ocasionalmente / 20% regulariza após notificação / 10% inadimplente. The active book stays between 40 and 60 contracts.

## Constraints

- The Carteira design HTML is the visual contract. Tokens, layout, table, markers, command line, refusals, dialog, and Portuguese copy in `ux.md` plus the adopted HTML must be replicated. Mock `<script type="text/x-dc">` generation, movement, outcomes, stream drops, and client-side guard evaluation do not ship.
- Where the brief and the design disagree on operator-visible rules, the design wins. Policy is **16+ simulated days late**, a prior notification **at least 48h (2 simulated days) old**, and the **device online**. Brief “30 days + 3 simulated days” is superseded. Armed expiry is **30s offline while `ARMED`**, not the rental 5s `SENT` window. Calendar default is **4h/s (`×14400`)**, not `SIM_DAY_SECONDS=300`. Map share is **`--map-share 42%`**; the mock’s 52/48 split is drift.
- Write-guard numbers the brief left blank are the designed ones: **20s** minimum interval per contract (`409`); **6 writes / 60s / IP** (`429` + `Retry-After`). Notify cooldown **24h simulated** and the payment window (**overdue or due within 5 simulated days**) are in the contract because the mock refuses on them.
- Policy is enforced on the server. `422` carries a readable error code. A refused button is never hidden: opacity `.45`, reason in 10.5px (`--text-3` for policy, `--warn` for interval, `--err` only for the rate-limit band).
- Block reason is 8–280 characters with a mono counter; confirm stays at opacity `.45` until the minimum. JSON write bodies max **8 KiB**.
- Fleets never mix in screens, logical MQTT topics, or queries. Leasing topics are `leasing/{vin}/telemetry|commands|ack`. HTTP and SSE: `architecture.md`. Block and vehicle machines: `state-machines.md`. Visual contract: `ux.md` plus the adopted design HTML. Phase-1 survival rules: `brownfield.md`.
- No login. Integrity is rate limit, interval, idempotency, validation, self-heal, and audit.
- Write routes require `Idempotency-Key` scoped to contract id + action (`notify` | `block` | `cancel` | `payment`). Same key replays the original record.
- Backend block states are English (`REQUESTED` … `TIMEOUT`); UI uses the design Portuguese labels.
- PostgreSQL, driver pgx, queries sqlc, migrations goose. Tables: `vehicles`, `vehicle_state`, `routes`, `customers`, `contracts`, `installments`, `payments`, `commands`, `audit_log`, `outbox`. `vehicle_state` upserts and is cached in memory. Command, audit, and outbox row commit in one transaction; a relay publishes MQTT and marks sent.
- Replace the phase-1 in-memory store implementation; keep the same consumer-owned interface.
- Route library is a versioned seed (~60 São Paulo points of interest, ~300 OSRM routes, generated offline once). Runtime never calls an external routing service. RNG seed is configurable. Incident scheduler: temporary offline, low battery, signal loss.
- Payer profiles weighted **40 / 30 / 20 / 10** (pontual, atrasa ocasionalmente, regulariza após notificação, inadimplente). Payments follow the profile at each due date. Active book stays 40–60. Self-heal floor is **10% per overdue band**. Max block duration is **30 simulated days**.
- Reachable leasing devices refuse about **15%** of block executions, in addition to the incident scheduler.
- Audit is append-only: real timestamp, simulated date, origin visitor|system, action, reason, previous state, new state, correlation id. Visitor identity is a hashed anonymous id from IP, never raw IP.
- MQTT is internal-only. Security headers. CORS is **same-origin only** (Caddy serves UI and API on one host). SSE is `GET /api/stream?fleet=rental|leasing`: one `EventSource`, server pushes only that fleet, missing/invalid `fleet` is `400`. Cap **64** simultaneous SSE connections. Drop **oldest** under pressure; no compression on the stream route.
- Calendar rate is environment-only (`1h/s`, `4h/s` default, `12h/s`). The UI shows the `×N` badge and does not expose a control.
- Routing: `react-router-dom`. `/` Frota, `/carteira` Carteira, `/carteira/:id` selected contract. Unknown paths still fall back to `index.html`.
- Carteira viewport is the mock Greater São Paulo bounds (south −23.63, north −23.49, west −46.87, east −46.41), not the rental Centro geofence.
- The rental fleet also uses PostgreSQL and the versioned route library. Its 5s unlock/lock machine is unchanged.
- `ARMED` never uses a spinner: dashed 26px ring breathes at 2.4s. Spinner only while the system is working (`REQUESTED`, `SENT`, reconnecting).
- Frontend stack, theme, tiles, ports, Go module rules, and embed: `architecture.md`. Rebuild a leasing marker only when vehicle state, selection, online, days-late, or heading rounded to 10° changes; otherwise `setLatLng` only.
- Authored repository text is English. Shipped UI copy is Portuguese as designed.
- Position history (leasing only, one-minute samples, 7-day retention) is optional and the **first scope cut**. Retention jobs bound audit and finished commands so disk stays stable across days.
- If the window slips, priority is: persistence + outbox, block machine, procedural sim, Carteira, demo protection, deploy.

## Non-goals

- Authentication, authorization, interest, fines, boletos, real SMS or email to the client, two-step block approval, multi-tenant, end-to-end tests, high availability.
- Operator OAuth 2.0, PostGIS, NATS fan-out, request-to-ack metrics (parked after phase 2).
- Changing the rental unlock/lock machine (still `PENDING`/`SENT`/`ACKED`/`FAILED`/`TIMEOUT`, 5s from `SENT`).
- Mobile responsiveness (designed min-width 1280px).
- Client-side simulation or client-side policy evaluation.
- Visitor-facing calendar speed control (rate is environment-only).

## Success signal

The evaluator opens `/carteira` (or a `/carteira/{id}` deep link), sees financed cars moving under a simulated date, is refused a premature block with the designed 16+/48h copy, arms a legal block and watches it wait until the car stops, then confirms immobilization. A payment releases the car, including after an offline reconnect. Spamming writes yields `429` and the book heals itself within 30 simulated days / the 10% band floor. Restarting the container keeps contracts, commands, and audit.
