# Fleet Pulse — leasing (Carteira) design import

Source: Claude Design project `1cd411ec-27c5-4950-99df-52e74e5f196c`
(<https://claude.ai/design/p/1cd411ec-27c5-4950-99df-52e74e5f196c>), imported on 2026-09-21.

These files are an imported design reference, not application code. They are kept verbatim as
exported. Shipped UI copy is Portuguese, matching the mock. Code, identifiers, comments and
authored documentation stay English.

The leasing screen is a second console on top of the same design system as the fleet screen in
`docs/fleet/design/`. It inherits every token defined there and adds only what the overdue-payment
domain needs.

## Files

| File | What it is |
|---|---|
| `Fleet Pulse Carteira.dc.html` | Interactive leasing mock: KPI row, band filters, map, contracts table, contract panel, audit trail, block dialog. |
| `Fleet Pulse Carteira Spec.dc.html` | Design spec: new tokens, table anatomy, leasing markers, block command states, action refusals, dialog. |
| `support.js` | Generated `dc-runtime` bundle that boots the `.dc.html` documents. Byte-identical to `docs/fleet/design/support.js`. Do not edit. |

Open either `.dc.html` directly in a browser. `support.js` must sit next to them, and both documents
pull React, Leaflet 1.9.4 and IBM Plex from CDNs, so viewing requires network access. The specs
cross-link to `Fleet Pulse.dc.html` and `Fleet Pulse Spec.dc.html`; those links only resolve if the
fleet documents sit in the same directory, which they do not here.

## What the design fixes

- **Tokens.** Three new colours, all for the overdue band: `--late-1 #F59E0B` (1–15 days),
  `--late-2 #F97316` (16–30), `--late-3 #EF4444` (>30). Plus translucent fills
  (`--accent-soft`, `--warn-soft`, `--err-soft`, `--ok-soft`), `--err-hover`, and layout tokens
  `--panel-w 380px`, `--kpi-h 56px`, `--filter-h 40px`, `--row-h 32px`, `--table-head-h 30px`,
  `--map-share 42%`.
- **Semantics.** Amber is "attention / waiting", red is "block / debt", blue stays action and
  selection. The *armed* state never uses a spinner — it uses a dashed ring breathing at 2.4s.
  A spinner only appears while the system is working (requested, sent, reconnecting).
- **Band readability without colour.** Every band carries three redundant signals: colour, number
  of filled segments (1/2/3) and the day count in mono. The "Próx. venc." column repeats the band
  colour so a row reads horizontally.
- **Layout.** Map on top (42% of the body), contracts table below, 380px contract panel on the
  right. The action grid is a fixed 2×2 at the panel footer; the rest of the panel scrolls.
- **Table.** Columns `Cliente · Placa/modelo · Parcela · Próx. venc. · Atraso · Veículo · Sinal`,
  32px rows, default sort by days late desc then client. Selection is a 2px `--accent` left bar
  plus `--accent-soft` background; a pending action is a 2px `--warn` left bar. Selecting a row
  pans the map if the vehicle is outside the viewport.
- **Marker.** Same car silhouette as the fleet screen, but a 56×44 `divIcon` with anchor
  `[28,12]`. The chip carries contract id plus days late (`L17 23d`) in the band colour; on-time
  contracts show the id alone in `--text-2`. Blocked is the only non-selected marker with a filled
  chip. Unlock-pending is a dimmed car with a dashed red stroke. `zIndexOffset`: active 0,
  others 200, selected 1000.
- **Simulated calendar.** A dashed chip with an inverted `DATA SIMULADA · ×14400` badge; the
  calendar advances 4h per second by default (`1h/s`, `4h/s`, `12h/s`), while the telemetry clock
  stays real time.
- **Degraded stream.** Identical treatment to the fleet screen: amber pill with retry counter,
  dashed band at the top of the map, frozen positions. Armed commands do not advance until the
  stream reconnects.

## Block command states in the mock

| Mock state | `COMANDO` line | Meaning |
|---|---|---|
| `solicitado` | `SOLICITADO` (accent, pulsing dot) | Dialog confirmed, ~0.9s. |
| `armado` | `ARMADO · aguardando veículo parar` (warn, breathing dot) | Waiting on a vehicle condition, not on I/O. Lasts minutes. Cancel stays available. |
| `enviado` | `ENVIADO · aguardando ACK` (accent, pulsing dot) | Vehicle stopped with ignition off and device online, ~2s. Cancel refused. |
| `confirmado` | `CONFIRMADO` (ok) | Vehicle becomes `Bloqueado`; movement reads `IMOBILIZADO`. |
| `cancelado` | `CANCELADO` (neutral) | Manual, or automatic once the debt is settled. Not an error. |
| `falhou` | `FALHOU` (err) | Device refused. Vehicle returns to `Ativo`. |
| `expirado` | `EXPIRADO` (warn) | Armed with the device offline for more than 30s. Amber, not red: nothing was done to the car. |

Vehicle states are `ativo`, `armado` (block armed), `bloqueado` and `desbloqueio_pendente`.

## Action guardrails in the mock

Four actions: `Notificar cliente` (primary), `Solicitar bloqueio` (danger, always via the dialog),
`Cancelar bloqueio` and `Registrar pagamento` (secondary). A refused button is never hidden — it
drops to opacity .45 and states the reason in 10.5px underneath.

- Policy refusals (reason in `--text-3`): requires 16+ days late, requires a prior notification,
  requires that notification to be at least 48h old, device must be online.
- Minimum interval (reason in `--warn`): 20s between actions on the same contract, with a
  countdown.
- Rate limit: 6 actions per 60s overall. A red band above the grid with the time until release;
  the only refusal rendered in red, because it blocks everything.
- Block dialog: 480px, mandatory reason of at least 8 characters with a mono counter, amber notice
  "O bloqueio não é imediato", confirm labelled `Confirmar solicitação de bloqueio`.

## Relation to the backend contract

Closed against `_bmad-output/specs/spec-leasing/`. Rental unlock/lock stays on
`PENDING` / `SENT` / `ACKED` / `FAILED` / `TIMEOUT` with a 5-second expiry from `SENT`.
Leasing is a distinct machine:

| Backend | Mock `COMANDO` |
|---|---|
| `REQUESTED` | `SOLICITADO` |
| `ARMED` | `ARMADO · aguardando veículo parar` |
| `SENT` | `ENVIADO · aguardando ACK` |
| `ACKED` | `CONFIRMADO` |
| `CANCELLED` | `CANCELADO` |
| `FAILED` | `FALHOU` |
| `TIMEOUT` | `EXPIRADO` |

`ARMED` waits for a stopped, ignition-off, online vehicle and is not bounded by the rental
5-second window. `TIMEOUT` is armed + offline for more than 30s.

Policy, interval, and rate limit are backend rules. The mock's client-side guards do not ship.

## Recorded implementation decisions

The mock remains visual truth. These decisions override mock-only behaviour and the original
brief where they conflicted.

1. **Policy.** 16+ simulated days late, a prior notification at least 48h old, device online.
   Notify cooldown 24h simulated. Payment allowed if an installment is overdue or due within
   5 simulated days. Reason 8–280 characters. JSON body max 8 KiB.
2. **Guards.** 20s minimum interval per contract (`409`). 6 writes / 60s / IP (`429` +
   `Retry-After`). Self-heal: 10% floor per overdue band; unblock after 30 simulated days.
3. **Calendar.** Environment-only rate (`1h/s`, `4h/s` default / `×14400`, `12h/s`). The chip
   is display, not a control. Telemetry clock stays real time. Brief `SIM_DAY_SECONDS=300`
   is superseded.
4. **Map share.** `--map-share 42%` as in the spec HTML. The mock's 52/48 split is drift.
5. **Viewport.** Mock Greater São Paulo bounds, not the rental Centro geofence.
6. **Routing.** Header tabs stay as designed. `react-router-dom` underneath: `/`, `/carteira`,
   `/carteira/:id`.
7. **SSE.** `GET /api/stream?fleet=rental|leasing`. One EventSource; server pushes only that
   fleet. Tab swap is a controlled reconnect, not `STREAM CAIU`.
8. **MQTT.** `leasing/{vin}/telemetry|commands|ack`. Fleets never share topics.
9. **Refuse rate.** About 15% on reachable leasing block executions, plus the incident scheduler.
10. **CORS.** Same-origin. Caddy serves UI and API on one host. SSE cap 64.

## Mock-only logic

Not part of any future build: the 50 contracts generated in the constructor, client-side
vehicle movement, the random block outcome, the editor-driven stream drop, the editor
calendar-speed props, and client-side guard evaluation. The visual chip, tokens, layout,
markers, dialog, and copy are the contract; the server owns calendar, motion, outcomes,
and refusals.
