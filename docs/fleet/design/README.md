# Fleet Pulse — design import

Source: Claude Design project `1cd411ec-27c5-4950-99df-52e74e5f196c`
(<https://claude.ai/design/p/1cd411ec-27c5-4950-99df-52e74e5f196c>), imported on 2026-09-20.

These files are an imported design reference, not application code. They are kept verbatim as
exported. Shipped UI copy is Portuguese, matching the mock. Code, identifiers, comments and
authored documentation stay English.

## Files

| File | What it is |
|---|---|
| `Fleet Pulse.dc.html` | Interactive console mock: header KPIs, Leaflet map with vehicle markers, vehicle panel, command flow, event feed. |
| `Fleet Pulse Spec.dc.html` | Design spec: tokens, marker anatomy, command states, vehicle/connection/empty states. |
| `support.js` | Generated `dc-runtime` bundle that boots the two `.dc.html` documents. Do not edit. |

Open either `.dc.html` directly in a browser. `support.js` must sit next to them, and both documents
pull React, Leaflet 1.9.4 and IBM Plex from CDNs, so viewing requires network access.

The project also holds `uploads/pasted-1789950923810-0.png`, a reference screenshot that neither
document links to. It was not imported.

## What the design fixes

- **Theme.** Dark by default with a light variant, both driven by CSS custom properties on
  `:root` / `:root[data-theme="light"]`. Radius is 2px, no shadows outside the marker.
- **Colour discipline.** One accent (blue) for action and selection; green/amber/red are semantic
  only. "Em uso" is deliberately a light neutral (`#C7CFDB`) so the most common moving state never
  competes with alerts.
- **Typography.** IBM Plex Sans for UI, IBM Plex Mono with tabular numerals for plates, KPIs,
  coordinates and timestamps.
- **Layout.** 44px header, `1fr / 360px` map-plus-panel body, collapsible event footer
  (172px open, 28px collapsed).
- **Marker.** A 44×44 Leaflet `divIcon`: a 14×24 top-down car SVG rotated to the heading, plus an
  id chip below it. Anchor `[22,12]`, `iconSize [44,44]`. Fill is always the vehicle state — the
  accent only ever appears as the selection ring and chip. The icon is rebuilt only when state,
  selection or heading (rounded to 10°) changes; otherwise only `setLatLng` runs.
- **Command feedback.** On acknowledgement exactly three things change: the pulsing ring stops, the
  `COMANDO` line switches colour, and the "Portas" row gets a 1.6s flash. One row slides into the
  feed. No toast, no modal.
- **Degraded stream.** Reconnection is amber, never red: a pill with a retry counter, a dashed 2px
  band at the top of the map, and a "positions frozen" notice. Markers stop moving and commands
  wait.

## Mapping to the backend contract

The mock labels map onto the command states in `CLAUDE.md` as follows:

| Backend state | Mock label | Panel colour |
|---|---|---|
| — (no command) | `OCIOSO` | `--text-3` |
| `PENDING` | `PENDENTE` | `--accent`, pulsing ring on the primary button |
| `SENT` | `ENVIADO · aguardando ACK` | `--accent`, ring stops, dot keeps pulsing |
| `ACKED` | `CONFIRMADO` | `--ok` |
| `FAILED` | `FALHOU` | `--err` |
| `TIMEOUT` | `EXPIROU` | `--warn` |

Vehicle states in the mock (`disponivel`, `em_uso`, `fora`, `offline`) are presentation states the
frontend derives from telemetry — the backend publishes telemetry, not these labels.

## Recorded implementation decisions

Closed against `_bmad-output/specs/spec-fleet-pulse/`. The mock remains visual truth; these
decisions override mock-only behaviour.

1. **Command expiry.** 5 seconds from `SENT`. UI copy follows 5s, not the mock's 15s/6s.
2. **Mock-only logic.** Vehicle generation, movement, geofence crossing, command outcomes and
   stream drops do not ship. The app reads `GET /api/vehicles`, `GET /api/stream` and
   `GET /api/commands/{id}`; the frontend only renders and selects.
3. **State container.** `useReducer` indexed by VIN; one `EventSource` guarded against StrictMode
   double mount.
4. **react-leaflet.** Target v5. Rebuild the icon only when state, selection or heading (rounded
   to 10°) changes.
5. **Tiles.** Esri Canvas World Gray, dark and light, attribution `© Esri, HERE, OpenStreetMap`.
6. **UI language.** Portuguese, as designed.
7. **Theme.** Dark default plus the console light-theme toggle (`fleetpulse-theme`).
8. **Lock and geofence.** Both are in MVP: `Travar` on the same command machine; Centro polygon
   with `Fora da área` state, KPI, legend and feed event.
