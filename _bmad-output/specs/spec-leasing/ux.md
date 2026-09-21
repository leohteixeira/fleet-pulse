# UX

Visual and interaction contract for Carteira. Pixel truth lives in the adopted HTML companions; this file is the decisions an implementer must not invent. Mock `<script type="text/x-dc">` logic does not ship. Shipped UI copy is Portuguese as designed.

Carteira is a second console on the same design system as Frota. It **inherits every Fleet token** and adds only overdue-band and leasing-layout tokens.

## Surfaces

Header tabs still look as designed. They are backed by `react-router-dom`: `/` Frota, `/carteira` Carteira, `/carteira/:id` selected contract. Selecting a row or marker navigates to `/carteira/{id}`. Close / map-background deselect navigates to `/carteira`. Unknown `{id}` keeps Carteira mounted, empty panel, no toast. Designed min-width 1280px, min-height 800px.

| Region | Size | Role |
|---|---|---|
| Header | 44px | Wordmark, tabs, simulated date, theme, stream pill |
| KPI row | `--kpi-h` 56px | Four KPIs + policy reminder |
| Filter row | `--filter-h` 40px | Bands, vehicle state, search |
| Body | `1fr / --panel-w 380px` | Map+table / contract panel |
| Map vs table | `--map-share 42%` / remainder | Map above, table below |

The interactive mock used a 52/48 map/table split. The contract is **42%**.

Header wordmark on Carteira: 8px accent square, `FLEET PULSE`, mono `SP · cobrança`. Tabs: `Frota` (inactive, `--text-2`) and `Carteira` (active, `--text-1`, 2px `--accent` underline).

KPI reminder (right of the four KPIs, `--text-3` 11.5px): `Bloqueio só é efetivado com o veículo parado e ignição desligada.`

## Theme and tokens

Dark default. Light via `:root[data-theme="light"]`. Same header toggle and `localStorage` key `fleetpulse-theme` as Frota. Radius 2px. No shadows except the marker drop-shadow.

IBM Plex Sans for UI. IBM Plex Mono with tabular numerals for ids, plates, money, dates, KPIs, coordinates, counters.

Semantic rule (fixed): **amber is attention / waiting**, **red is block / debt**, **blue stays action and selection**. `ARMED` never uses a spinner.

Inherited Fleet tokens stay as in `../spec-fleet-pulse/ux.md` and `docs/fleet/design`. Carteira adds:

| Token | Dark | Use |
|---|---|---|
| `--late-1` | `#F59E0B` | 1–15 days, ▮▯▯ |
| `--late-2` | `#F97316` | 16–30 days, ▮▮▯ |
| `--late-3` | `#EF4444` | >30 days, ▮▮▮ |
| `--accent-soft` | `rgba(59,130,246,.08)` | selected row / command fill |
| `--warn-soft` | `rgba(245,158,11,.12)` | armed fill |
| `--err-soft` | `rgba(239,68,68,.12)` | blocked / rate-limit fill |
| `--ok-soft` | `rgba(52,211,153,.12)` | confirmed fill |
| `--err-hover` | `#DC2626` | dialog confirm hover |
| `--v-active` | `var(--v-in-use)` | `#C7CFDB` active car |
| `--v-armed` | `var(--warn)` | armed car |
| `--v-blocked` | `var(--err)` | blocked car |
| `--v-unlock-pending` | `--v-offline` + dashed `--err` stroke | pending unlock |
| `--panel-w` | `380px` | |
| `--kpi-h` / `--filter-h` | `56px` / `40px` | |
| `--row-h` / `--table-head-h` | `32px` / `30px` | |
| `--map-share` | `42%` | |
| `--bg-3` | `#1F2632` | selected filter chip |
| `--line-strong` | `#3A4353` | dialog, secondary borders, empty dash |
| `--overlay` / `--chip` / `--chip-border` / `--glass` | from adopted HTML `:root` | map chrome, marker chip, windows |

Light overdue tokens from the mock: `--late-1 #B45309`, `--late-2 #C2410C`, `--late-3 #DC2626`. Remaining light values: copy from the adopted `Fleet Pulse Carteira.dc.html` `:root[data-theme="light"]`. Do not invent `--bg-3`, `--line-strong`, or chip/overlay tokens.

Animations (same names as the mock): `fp-live` pulse 1.6s, `fp-armed` breathe 2.4s (opacity .35→1, scale .94→1), `fp-spin` .8s, `fp-pending` ring 1.2s, `fp-flash-ok` / `fp-flash-err` 1.6s, `fp-row-in` 4px slide .25s.

## Simulated calendar

Dashed amber chip in the header: 5px dashed circle + uppercase `Data simulada · ×N` (default `×14400`) + mono simulated weekday and datetime. Real date is the tooltip / secondary, `--text-3`. Calendar rate is environment-only (`1h/s`, `4h/s` default, `12h/s`); the chip is display, not a control. Stream clock stays real time (`HH:MM:SS` on the live pill).

## KPIs

Four cells, then the policy reminder in the `--panel-w` column.

| Label | Value | Sub |
|---|---|---|
| Contratos ativos | count | `{n} em atraso` |
| Valor total em atraso | `pt-BR` currency | `{n} parcelas` — value `--err` when > 0 |
| Bloqueios armados | count | `aguardando parada` — value `--warn` when > 0 |
| Veículos bloqueados | count | `{n} desbloq. pendente` |

## Filters

- **Atraso** chips: Todos, Em dia, 1–15 dias, 16–30 dias, > 30 dias. Selected chip `--bg-3` / `--text-1`. Each chip shows a 6px band square and a mono count.
- **Veículo** select: Todos, Ativo, Bloqueio armado, Bloqueado, Desbloqueio pendente.
- Search placeholder `Placa ou cliente`, width 220px, height 28px. Matches plate, client, or contract id.
- Right: `{visible} de {total} contratos` in mono `--text-3`.

Empty table: 36×20 dashed rectangle, `Nenhum contrato corresponde aos filtros`, hint (`Nenhuma placa ou cliente contém “{q}”.` or `Combine outra faixa de atraso ou estado do veículo.`), button `Limpar filtros`. No illustration.

## Table

Columns, uppercase 10.5px `--text-3`: `Cliente · Placa · modelo · Parcela · Próx. venc. · Atraso · Veículo · Sinal`.

Row height 32px. Head 30px. Default sort: days late desc, then client.

| Column | Treatment |
|---|---|
| Cliente | Mono id `L17` in `--text-3` + name |
| Placa · modelo | Mono plate 500 + model `--text-2` |
| Parcela | Mono tabular, right |
| Próx. venc. | Mono; **repeats the band colour** when late, else `--text-2` |
| Atraso | 3 segments 5×12 + mono days (`9d`, `23d`, `47d`, or `em dia`). Weight 600 only on >30 |
| Veículo | State pill + optional pending mono label |
| Sinal | Online: `--ok` dot + `online` `--text-2`. Offline: `--text-3` dot + `offline 42min` in `--warn`. Offline does **not** recede the whole row |

Selection: 2px `--accent` left bar + `--accent-soft` fill. Pending action: 2px `--warn` left bar. Hover: `--bg-2`.

Pending labels: `solicitado`, `aguardando parar`, `enviado · ACK`, `desbloqueio enviado`. Armed pill dot uses `fp-armed`, not a blink.

Selecting a row `panTo`s the map if the vehicle is outside the viewport (pad −0.1). Selecting a marker scrolls the row into view (32px row math).

Each band has **three redundant signals**: colour, filled segment count (1/2/3), and the day count in mono. Do not rely on colour alone.

## Marker

56×44 Leaflet `divIcon`, anchor `[28,12]`. Same 14×24 top-down car SVG as Frota (path in the adopted spec HTML). Chip below, IBM Plex Mono 600 10px.

Chip text: `L17 23d` in the band colour; on-time shows the id alone in `--text-2`. Blocked is the **only** non-selected marker with a filled chip (`--err` fill, white text). Selected chip is `--accent` fill, white text.

| Presentation | Fill | Extra |
|---|---|---|
| Ativo · em dia | `--v-in-use` | grey chip |
| Ativo · late | `--v-in-use` | chip in band colour |
| Bloqueio armado | `--warn` | 26px dashed `--warn` ring, `fp-armed` 2.4s. Never a spinner |
| Bloqueado | `--err` | inverted chip |
| Desbloqueio pendente | `--v-offline` | dashed `--err` stroke, red-tinted glass |
| Any + device offline | `--v-offline` if ativo | dashed `--text-3` stroke, dim glass, opacity .8 |
| Selected | state fill | 32px 2px `--accent` ring + glow |

`zIndexOffset`: ativo 0, others 200, selected 1000.

Rebuild the icon only when vehicle state, selection, online, days-late, or heading rounded to 10° changes. Otherwise `setLatLng` only. No CSS position transition.

Map chrome, bottom-left overlay: Ativo, Bloqueio armado (dashed ring), Bloqueado, Desbloqueio pendente, Offline, `etiqueta: contrato · dias em atraso`.

Tiles: Esri Canvas World Gray dark/light, attribution `© Esri, HERE, OpenStreetMap`. Viewport: mock Greater São Paulo (south −23.63, north −23.49, west −46.87, east −46.41). This is not the rental Centro geofence.

## Panel

Empty: 36px dashed crosshair, `Nenhum contrato selecionado`, `Selecione uma linha da tabela ou um marcador no mapa para ver o contrato e agir.`, mono hint `{n} contratos com 16+ dias e veículo ativo`.

Selected header: client 15/600 + mono id, plate + model, vehicle-state pill, close `×`.

Fact grid:

- Em atraso: mono 20/600 amount (`--err` if > 0) + band segments + `{n} dias · {band}`
- Parcela: mono 20/600 + `próx. {date}`
- Parcelas pagas `{paid} / {total}` and a 4px bar: `--ok` paid share, `--err` overdue share

Estado do veículo rows: Movimento (`IMOBILIZADO` / `EM MOVIMENTO · {n} km/h` / `PARADO`), Ignição (`LIGADA` / `DESLIGADA`), Última posição (mono 5 decimals), Último sinal (`ONLINE`/`OFFLINE` · relative time).

Histórico de parcelas: 26px mono rows, index, due date, value, status `paga` / `vencida` / `aberta` (`--ok` / `--err` / `--text-3`). Show a window around current (paid−2 .. paid+overdue+2).

Auditoria: vertical timeline, origin `VISITANTE` (`--accent`) or `SISTEMA` (`--text-3`), action, reason. New row: `fp-row-in`. Colour action by kind: fail/expire/confirm-block/overdue in `--err` or `--warn`; payment/unlock confirm `--ok`.

The action grid is a **fixed 2×2 at the panel footer**; the rest of the panel scrolls.

## Actions and refusals

| Slot | Kind | Idle label |
|---|---|---|
| 1 | primary `--accent` | `Notificar cliente` |
| 2 | danger `--err` | `Solicitar bloqueio` — **always** via the dialog |
| 3 | secondary | `Cancelar bloqueio` |
| 4 | secondary | `Registrar pagamento` |

Button height 34px. Refused: never hidden; opacity `.45`; reason 10.5px underneath.

Policy reasons (`--text-3`):

- `Política: exige 16+ dias em atraso (atual {n})`
- `Política: notificação prévia obrigatória`
- `Política: notificação há menos de 48h`
- `Dispositivo offline · comando não chegaria`
- `Contrato em dia` / `Notificado há menos de 24h`
- `Veículo já bloqueado` / `Desbloqueio pendente` / `Nenhum bloqueio ativo ou armado`
- `Sem parcela vencida ou a vencer em 5 dias` / `Contrato quitado`

Interval (`--warn`): `Aguardando intervalo entre ações · {n}s`. Applies to all four buttons. 20s on the same contract.

Rate limit (the **only** red refusal, because it blocks everything): band above the grid, `Limite de requisições atingido · libera em {n}s`. All buttons disabled with that reason. 6 actions / 60s.

While `ARMED`, danger label becomes `Bloqueio armado` with `Aguardando veículo parar para efetivar`. While `REQUESTED`/`SENT`, danger shows spinner + `Enviando…` and `fp-pending`.

Idle hints (not refusals): notify `última {date}` or `nenhuma notificação enviada`; block `exige confirmação e motivo`; cancel on blocked `envia desbloqueio`; payment `quita a parcela mais antiga`.

Treat `409` / `422` / `429` from the server with these same strings. Do not re-implement policy in the browser as the source of truth.

## Command line

Always visible under the 2×2 when a contract is selected. Mono 11px.

| Backend | `COMANDO` | Colour | Dot |
|---|---|---|---|
| none | `OCIOSO` | `--text-3` | none |
| `REQUESTED` | `SOLICITADO` | `--accent` | `fp-live` |
| `ARMED` | `ARMADO · aguardando veículo parar` | `--warn` | `fp-armed` |
| `SENT` | `ENVIADO · aguardando ACK` | `--accent` | `fp-live` |
| `ACKED` | `CONFIRMADO` | `--ok` | none |
| `CANCELLED` | `CANCELADO` | `--text-2` | none |
| `FAILED` | `FALHOU` | `--err` | none |
| `TIMEOUT` | `EXPIRADO` | `--warn` | none |

Unlock in flight prefixes `DESBLOQUEIO · `. No toast. No modal except the block dialog. Armed is the central state: danger button, vehicle pill, marker, table bar, and `COMANDO` line all go amber together.

## Block dialog

480px, `--bg-1`, border `--line-strong`, overlay `rgba(11,14,19,.72)`. Title square `--err` + `Solicitar bloqueio remoto` + mono contract id.

Fact grid: Cliente, Veículo (plate + model), Em atraso (amount · **days in `--err`**), Notificação prévia (datetime + `há N dias` / `nunca`).

Motivo `obrigatório` (`--err`). Placeholder `Ex.: 3 tentativas de contato sem retorno; cliente ciente do risco de bloqueio.` Counter `N / mín. 8 caracteres`. Confirm stays opacity `.45` until 8 trimmed characters.

Amber notice with the armed dashed-ring glyph: **`O bloqueio não é imediato.`** `O comando fica armado e só é efetivado quando o veículo estiver parado com a ignição desligada. Até então o cliente pode circular normalmente e você pode cancelar.`

Buttons: `Cancelar` secondary; confirm `--err` / hover `--err-hover`, label **`Confirmar solicitação de bloqueio`**.

## Stream

Identical to Frota when the connection drops.

- Live: green 7px `fp-live`, `STREAM · AO VIVO`, ticking real clock.
- Degraded: amber pill `STREAM CAIU · RECONECTANDO (n/8)`, 2px dashed amber band at top of map, overlay `Posições congeladas · último dado {rel}`. Markers freeze. `ARMED` does not advance. Never red.
- Tab switch opens a new `EventSource` with the other `?fleet=`. That swap is controlled: keep the live pill, do not increment the retry counter, do not paint the dashed band.

## Language

Shipped strings are Portuguese as designed. Code, identifiers, comments, and authored repository docs stay English.
