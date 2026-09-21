# UX

Visual and interaction contract. Pixel truth lives in the adopted HTML companions; this file is the decisions an implementer must not invent. Mock `<script type="text/x-dc">` logic does not ship. Shipped UI copy is Portuguese.

## Surfaces

One screen. No router.

| Region | Size | Role |
|---|---|---|
| Header | 44px | Wordmark, KPIs, theme toggle, stream pill |
| Body | `1fr / 360px` | Map + vehicle panel |
| Feed | 172px open, 28px collapsed | Event log |

Header KPIs: Veículos, Disponíveis, Em uso, Fora, Offline, Pendentes.

## Theme and tokens

Dark default. Light variant via `:root[data-theme="light"]`. Header toggle matches the console mock; persist in `localStorage` key `fleetpulse-theme`. Radius 2px. No shadows except the marker drop-shadow. One accent (blue) for action and selection; green / amber / red are semantic only. "Em uso" fill is `#C7CFDB` (dark) / `#3F4A5C` (light) so the common moving state never competes with alerts.

IBM Plex Sans for UI. IBM Plex Mono with tabular numerals for plates, KPIs, coordinates, timestamps.

Dark tokens:

| Token | Value | Use |
|---|---|---|
| `--bg-0` | `#0B0E13` | page, map |
| `--bg-1` | `#11151C` | header, panel, feed |
| `--bg-2` | `#181D26` | secondary button, row hover |
| `--line` | `#232A36` | strong dividers |
| `--line-soft` | `#1A2029` | row rules |
| `--text-1` | `#E6EAF0` | primary |
| `--text-2` | `#8B95A5` | secondary |
| `--text-3` | `#5B6472` | muted, idle command |
| `--accent` | `#3B82F6` | action, selection |
| `--accent-hover` | `#2F6FDD` | |
| `--ok` | `#34D399` | available, acked |
| `--warn` | `#F59E0B` | outside, timeout, reconnect |
| `--err` | `#EF4444` | failed only |
| `--v-in-use` | `#C7CFDB` | in-use fill |
| `--v-offline` | `#2A313D` | offline fill |
| `--header-h` | `44px` | |
| `--panel-w` | `360px` | |
| `--feed-h` / `--feed-collapsed-h` | `172px` / `28px` | |

Light tokens (from the console mock): `--bg-0 #E9ECF0`, `--bg-1 #FFFFFF`, `--bg-2 #F3F5F8`, `--line #D5DAE2`, `--text-1 #141820`, `--text-2 #5B6472`, `--text-3 #6B7585`, `--accent #2563EB`, `--ok #059669`, `--warn #B45309`, `--err #DC2626`, `--v-in-use #3F4A5C`, `--v-offline #C9CFD8`. Remaining light values: copy from `Fleet Pulse.dc.html`.

## Marker

44×44 Leaflet `divIcon`. Car SVG 14×24, top-down, rotated to heading. Id chip below. Anchor `[22,12]`, `iconSize [44,44]`. Fill is always vehicle state; accent is selection ring (32px, 2px) and selected chip only. `zIndexOffset` 1000 when selected.

| Presentation state | Fill | Notes |
|---|---|---|
| disponível | `--ok` | parked, locked, ignition off |
| em_uso | `--v-in-use` | moving; heading arrow is the car itself |
| fora | `--warn` | last position outside the allowed polygon |
| offline | `--v-offline` | last-seen older than 60s, or the permanently silent vehicle; dashed `--text-3` stroke, dim windows, chip `--text-2`, opacity 0.85 |

Frontend **derives** these labels from telemetry plus the snapshot polygon. Backend does not publish them. `disponivel` vs `em_uso` from motion / ignition / locked as the mock does: parked, locked, ignition off → disponível; moving → em uso.

Rebuild the icon only when state, selection, or heading rounded to 10° changes. Otherwise `setLatLng` only. No CSS position transition. Parked cars still point their last heading.

SVG path and chip typography: copy from `Fleet Pulse Spec.dc.html`.

## Command feedback

| Backend | Label | Colour | Primary button |
|---|---|---|---|
| none | `OCIOSO` | `--text-3` | Destravar |
| `PENDING` | `PENDENTE` | `--accent`, 1.2s pulse ring on primary, spinner | Enviando… |
| `SENT` | `ENVIADO · aguardando ACK` | `--accent`, ring stops, 6px dot keeps pulsing | Aguardando… |
| `ACKED` | `CONFIRMADO` | `--ok` | re-enabled |
| `FAILED` | `FALHOU` | `--err` — only red on the panel | re-enabled |
| `TIMEOUT` | `EXPIROU` | `--warn` | re-enabled |

Secondary button label is `Travar`. While a command is in-flight, the **other** action stays enabled so the operator can queue it. The same action stays disabled at 0.6 opacity. After one command is queued, both buttons disable until that queued command becomes in-flight. The `COMANDO` line shows only the in-flight command; when it terminals, the queued one appears as `PENDENTE` and starts its cycle.

On acknowledgement, **exactly three** things change: the pulse ring stops; the `COMANDO` line switches colour; the Portas row gets a 1.6s flash (`fp-ok` on success → `DESTRAVADAS` or `TRAVADAS`, `fp-err` on failure). One row slides 4px into the feed. No toast, no modal.

Timeout copy must say **5 segundos**, not the mock's 15s/6s.

## Stream

- Live: green 7px pulse (1.6s), `STREAM · AO VIVO`, ticking clock.
- Degraded: amber pill `STREAM CAIU · RECONECTANDO (n/8)`, 2px dashed amber band at top of map, overlay `Posições congeladas · último dado há Ns`. Markers freeze. Commands wait. Never red.

## Panel

Empty: 36px dashed crosshair, "Nenhum veículo selecionado", "Clique em um marcador no mapa para ver telemetria e enviar comandos." No illustration.

Selected: plate (mono 20/600) + short id, model, state chip, close. Grid: battery (bar; <20% `--err`, <40% `--warn`) and speed + heading. Rows: Ignição, Portas, Quilometragem + trip, Último sinal (colour by age: >60s `--err`, >15s `--warn`), Posição.

Last-seen and coordinates use mono 11 tabular.

## Feed

Collapsible. Header 28px: chevron, Eventos, count, fail count, fora count. Rows 26px: relative time, vehicle id (selects), type chip, description. New row: 4px slide. Hover `--bg-2`.

## Map chrome

Legend bottom-left: Disponível, Em uso, Fora da área, Offline, "carro aponta o rumo", dashed "Área permitida". Draw the allowed polygon as a dashed `--text-3` overlay from the snapshot.

Tiles: Esri Canvas World Gray, dark or light with the theme. Attribution: `© Esri, HERE, OpenStreetMap`.

## Language

Shipped strings are Portuguese as designed. Code, identifiers, comments, and authored repository docs stay English.
