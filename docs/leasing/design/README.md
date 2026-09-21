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

The current MVP contract in `CLAUDE.md` covers `unlock`/`lock` with states `PENDING`, `SENT`,
`ACKED`, `FAILED`, `TIMEOUT` and a 5-second expiry. The leasing screen is a superset and is **not
yet reflected** in `openapi.yaml`:

- A block command waits for a vehicle condition (stopped, ignition off) before it is sent. That is
  the `armado` state, which has no equivalent in the fleet command machine and is not bounded by
  the 5-second expiry — expiry here is the device being offline for more than 30s while armed.
- Contracts, instalments, overdue amounts, bands, notifications and the audit trail are domain data
  the current API does not expose.
- Policy, minimum interval and rate limiting are refusals the mock evaluates client-side; they are
  business rules and belong in the backend.

Nothing in this directory has been implemented yet. Before writing code, the command machine,
contract resource and refusal rules need to be settled and recorded in `specs/` and `openapi.yaml`.

## Mock-only logic

Not part of any future build: the 50 contracts generated in the constructor, the simulated
calendar, vehicle movement, the random block outcome, the stream drop and the client-side guard
evaluation. The mock is visual truth only.
