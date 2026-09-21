import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

import { BlockDialog } from './BlockDialog';
import { CarteiraMap } from './CarteiraMap';
import { CarteiraPanel, type ActionKey } from './CarteiraPanel';
import { CarteiraTable } from './CarteiraTable';
import {
  loadContractDetail,
  loadContracts,
  loadLeasingVehicles,
  postBlock,
  postCancel,
  postNotify,
  postPayment,
} from './carteiraApi';
import { COPY, runeCount, type WriteRefusal } from './carteiraCopy';
import {
  applyVehiclePatch,
  BAND_LABEL,
  bandOf,
  carteiraKpis,
  filterRows,
  lateColor,
  mergeBook,
  moneyBRL,
  overdueFromInstallments,
  parseLeaseCommand,
  parseLeasePatch,
  lastNotifyAt,
  type BandFilter,
  type BookRow,
  type Contract,
  type ContractDetail,
  type LeaseCommand,
  type VehicleFilter,
} from './carteiraState';
import { ConsoleHeader } from './Header';
import { carteiraPath, resolveSelected } from './routes';
import { FREEZE_COPY } from './stream';
import { useStreamBus } from './streamContext';
import type { Vehicle } from './state';

const BANDS: BandFilter[] = ['all', 'em_dia', '1_15', '16_30', 'acima_30'];

export function Carteira() {
  const { id } = useParams<{ id?: string }>();
  const navigate = useNavigate();
  const { status, lastDataAt, now, theme, setTheme, clock, subscribe } = useStreamBus();
  const [contracts, setContracts] = useState<Contract[]>([]);
  const [vehicles, setVehicles] = useState<Record<string, Vehicle>>({});
  const [commands, setCommands] = useState<Record<string, LeaseCommand>>({});
  const [details, setDetails] = useState<Record<string, ContractDetail>>({});
  const [band, setBand] = useState<BandFilter>('all');
  const [vehicleState, setVehicleState] = useState<VehicleFilter>('all');
  const [query, setQuery] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [panId, setPanId] = useState<string | null>(null);
  const [scrollId, setScrollId] = useState<string | null>(null);
  const [dialog, setDialog] = useState(false);
  const [reason, setReason] = useState('');
  const [refusals, setRefusals] = useState<Partial<Record<ActionKey, WriteRefusal>>>({});
  const [interval, setIntervalRefusal] = useState<{ id: string; refusal: WriteRefusal } | null>(null);
  const [rate, setRate] = useState<WriteRefusal | null>(null);
  const [busy, setBusy] = useState<ActionKey | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void Promise.all([loadContracts(controller.signal), loadLeasingVehicles(controller.signal)])
      .then(([list, roster]) => {
        setContracts(list);
        const next: Record<string, Vehicle> = {};
        for (const vehicle of roster) {
          next[vehicle.vin] = {
            vin: vehicle.vin,
            displayId: vehicle.displayId ?? '',
            lat: vehicle.lat ?? 0,
            lng: vehicle.lng ?? 0,
            plate: vehicle.plate ?? '',
            model: vehicle.model ?? '',
            battery: vehicle.battery ?? 0,
            speed: vehicle.speed ?? 0,
            heading: vehicle.heading ?? 0,
            ignition: vehicle.ignition ?? false,
            locked: vehicle.locked ?? false,
            odometer: vehicle.odometer ?? 0,
            trip: vehicle.trip ?? 0,
            lastSeen: null,
          };
        }
        setVehicles(next);
        setError(null);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setError(err instanceof Error ? err.message : 'Não foi possível carregar a carteira.');
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    return subscribe((type, data) => {
      if (type === 'telemetry') {
        const patch = parseLeasePatch(data);
        if (!patch) {
          return;
        }
        setVehicles((prev) => {
          const current = prev[patch.vin];
          if (!current) {
            return prev;
          }
          return { ...prev, [patch.vin]: applyVehiclePatch(current, patch, Date.now()) };
        });
        return;
      }
      if (type === 'command') {
        const rec = parseLeaseCommand(data);
        if (!rec) {
          return;
        }
        setCommands((prev) => ({ ...prev, [rec.vin]: rec }));
      }
    });
  }, [subscribe]);

  const rows = useMemo(
    () => mergeBook(contracts, Object.values(vehicles), commands, now),
    [commands, contracts, now, vehicles],
  );
  const visible = useMemo(
    () => filterRows(rows, band, vehicleState, query),
    [band, query, rows, vehicleState],
  );
  const { selected } = resolveSelected(rows, id ?? null);
  const selectedDetail = selected ? (details[selected.id] ?? null) : null;

  const selectedId = id ?? null;
  useEffect(() => {
    if (!selectedId) {
      return;
    }
    let cancelled = false;
    void loadContractDetail(selectedId)
      .then((detail) => {
        if (!cancelled && detail) {
          setDetails((prev) => ({ ...prev, [detail.id]: detail }));
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [selectedId]);

  useEffect(() => {
    const missing = visible.filter((row) => !details[row.id]).slice(0, 8);
    if (missing.length === 0) {
      return;
    }
    const controller = new AbortController();
    void Promise.all(missing.map((row) => loadContractDetail(row.id, controller.signal))).then(
      (loaded) => {
        setDetails((prev) => {
          const next = { ...prev };
          for (const detail of loaded) {
            if (detail) {
              next[detail.id] = detail;
            }
          }
          return next;
        });
      },
    );
    return () => controller.abort();
  }, [details, visible]);

  useEffect(() => {
    if (!interval && !rate) {
      return;
    }
    const timer = window.setInterval(() => {
      setIntervalRefusal((prev) => {
        if (!prev) {
          return null;
        }
        const next = tickRefusal(prev.refusal);
        return next ? { id: prev.id, refusal: next } : null;
      });
      setRate((prev) => tickRefusal(prev));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [interval, rate]);

  const kpis = carteiraKpis(rows, details);
  const bandCounts = countBands(rows);

  const select = (nextId: string | null, from: 'map' | 'table' | 'close') => {
    navigate(carteiraPath(nextId));
    if (from === 'table' && nextId) {
      setPanId(nextId);
      setScrollId(null);
    }
    if (from === 'map' && nextId) {
      setScrollId(nextId);
      setPanId(null);
    }
    if (!nextId) {
      setPanId(null);
      setScrollId(null);
    }
  };

  const applyOutcome = async (key: ActionKey, run: () => ReturnType<typeof postNotify>) => {
    if (!selected) {
      return;
    }
    setBusy(key);
    const result = await run();
    setBusy(null);
    if (result.ok) {
      setRefusals({});
      setIntervalRefusal(null);
      setRate(null);
      const body = result.body;
      if (key === 'block' && isRecord(body) && typeof body.id === 'string') {
        const commandId = body.id;
        const state =
          body.state === 'REQUESTED' ||
          body.state === 'ARMED' ||
          body.state === 'SENT' ||
          body.state === 'ACKED'
            ? body.state
            : 'REQUESTED';
        setCommands((prev) => ({
          ...prev,
          [selected.vin]: {
            id: commandId,
            vin: selected.vin,
            action: 'block',
            state,
          },
        }));
      }
      const detail = await loadContractDetail(selected.id);
      if (detail) {
        setDetails((prev) => ({ ...prev, [detail.id]: detail }));
        setContracts((prev) =>
          prev.map((row) => (row.id === detail.id ? { ...row, ...detail } : row)),
        );
      }
      return;
    }
    if (result.refusal.kind === 'rate') {
      setRate(result.refusal);
      return;
    }
    if (result.refusal.kind === 'interval') {
      setIntervalRefusal({ id: selected.id, refusal: result.refusal });
      return;
    }
    setRefusals((prev) => ({ ...prev, [key]: result.refusal }));
  };

  const onNotify = () => {
    if (!selected) {
      return;
    }
    void applyOutcome('notify', () => postNotify(selected.id, selected.daysLate));
  };
  const onCancel = () => {
    if (!selected) {
      return;
    }
    void applyOutcome('cancel', () => postCancel(selected.id, selected.daysLate));
  };
  const onPay = () => {
    if (!selected) {
      return;
    }
    void applyOutcome('pay', () => postPayment(selected.id, selected.daysLate));
  };
  const confirmBlock = () => {
    if (!selected || runeCount(reason.trim()) < 8 || runeCount(reason.trim()) > 280) {
      return;
    }
    setDialog(false);
    void applyOutcome('block', () => postBlock(selected.id, reason.trim(), selected.daysLate));
    setReason('');
  };

  const overdueAmount = selected
    ? overdueFromInstallments(selectedDetail?.installments ?? [], selected.daysLate).amount
    : 0;

  return (
    <div className="shell carteira">
      <ConsoleHeader
        variant="carteira"
        theme={theme}
        onTheme={setTheme}
        stream={status}
        now={now}
        clock={clock}
      />
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : (
        <>
          <div className="ct-kpis">
            <Kpi
              label={COPY.kpiActive}
              value={String(kpis.active)}
              sub={COPY.kpiLate(kpis.late)}
              color="var(--text-2)"
            />
            <Kpi
              label={COPY.kpiOverdue}
              value={moneyBRL(kpis.overdueAmount)}
              sub={COPY.kpiInstallments(kpis.overdueInstallments)}
              color="var(--text-2)"
              valueColor={kpis.overdueAmount > 0 ? 'var(--err)' : undefined}
            />
            <Kpi
              label={COPY.kpiArmed}
              value={String(kpis.armed)}
              sub={COPY.kpiArmedSub}
              color="var(--warn)"
              valueColor={kpis.armed > 0 ? 'var(--warn)' : undefined}
            />
            <Kpi
              label={COPY.kpiBlocked}
              value={String(kpis.blocked)}
              sub={COPY.kpiUnlockPending(kpis.unlockPending)}
              color="var(--err)"
            />
            <div className="ct-reminder">{COPY.kpiReminder}</div>
          </div>
          <div className="ct-filters">
            <span className="ct-filter-lab">{COPY.filterLate}</span>
            <div className="band-chips">
              {BANDS.map((key) => (
                <button
                  key={key}
                  type="button"
                  className={`band-chip${band === key ? ' is-on' : ''}`}
                  onClick={() => setBand(key)}
                >
                  <i
                    style={{
                      background: key === 'all' ? 'var(--text-3)' : lateColor(key),
                    }}
                  />
                  {key === 'all' ? COPY.bandAll : BAND_LABEL[key]}
                  <span className="mono">{bandCounts[key]}</span>
                </button>
              ))}
            </div>
            <span className="ct-sep" />
            <span className="ct-filter-lab">{COPY.filterVehicle}</span>
            <select
              value={vehicleState}
              onChange={(event) => setVehicleState(event.target.value as VehicleFilter)}
            >
              <option value="all">{COPY.vsAll}</option>
              <option value="ativo">{COPY.vsAtivo}</option>
              <option value="armado">{COPY.vsArmado}</option>
              <option value="bloqueado">{COPY.vsBloqueado}</option>
              <option value="desbloqueio_pendente">{COPY.vsUnlock}</option>
            </select>
            <span className="ct-sep" />
            <input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={COPY.searchPlaceholder}
              className="ct-search"
            />
            <span className="ct-count">
              {COPY.visibleOf(visible.length, rows.length)}
            </span>
          </div>
          <div className="ct-body">
            <div className="ct-stack">
              <CarteiraMap
                rows={visible}
                selectedId={selected?.id ?? null}
                theme={theme}
                frozen={!status.live}
                freezeCopy={FREEZE_COPY(Math.max(0, Math.round((now - lastDataAt) / 1000)))}
                panId={panId}
                onPanDone={() => setPanId(null)}
                onSelect={(next) => select(next, next ? 'map' : 'close')}
              />
              <CarteiraTable
                rows={visible}
                details={details}
                selectedId={selected?.id ?? null}
                scrollId={scrollId}
                onScrollDone={() => setScrollId(null)}
                query={query}
                onSelect={(next) => select(next, 'table')}
                onClear={() => {
                  setBand('all');
                  setVehicleState('all');
                  setQuery('');
                }}
              />
            </div>
            <CarteiraPanel
              row={selected}
              detail={selectedDetail}
              rows={rows}
              clock={clock}
              now={now}
              refusals={refusals}
              rate={rate}
              interval={selected && interval?.id === selected.id ? interval.refusal : null}
              busy={busy}
              onClose={() => select(null, 'close')}
              onNotify={onNotify}
              onBlock={() => {
                if (selected) {
                  setDialog(true);
                }
              }}
              onCancel={onCancel}
              onPay={onPay}
            />
          </div>
        </>
      )}
      {dialog && selected ? (
        <BlockDialog
          row={selected}
          amount={overdueAmount}
          lastNotify={lastNotifyAt(selectedDetail)}
          clock={clock}
          reason={reason}
          onReason={setReason}
          onCancel={() => {
            setDialog(false);
            setReason('');
          }}
          onConfirm={confirmBlock}
        />
      ) : null}
    </div>
  );
}

function Kpi({
  label,
  value,
  sub,
  color,
  valueColor,
}: {
  label: string;
  value: string;
  sub: string;
  color: string;
  valueColor?: string;
}) {
  return (
    <div className="ct-kpi">
      <span className="ct-kpi-lab">
        <i style={{ background: color }} />
        {label}
      </span>
      <span className="ct-kpi-val">
        <span className="mono" style={valueColor ? { color: valueColor } : undefined}>
          {value}
        </span>
        <span className="mono muted">{sub}</span>
      </span>
    </div>
  );
}

function countBands(rows: BookRow[]): Record<BandFilter, number> {
  const counts: Record<BandFilter, number> = {
    all: rows.length,
    em_dia: 0,
    '1_15': 0,
    '16_30': 0,
    acima_30: 0,
  };
  for (const row of rows) {
    const key = row.overdueBand || bandOf(row.daysLate);
    counts[key] += 1;
  }
  return counts;
}

function tickRefusal(prev: WriteRefusal | null): WriteRefusal | null {
  if (!prev || prev.retryAfter <= 1) {
    return null;
  }
  const retryAfter = prev.retryAfter - 1;
  return {
    ...prev,
    retryAfter,
    copy: prev.kind === 'rate' ? COPY.rateLimit(retryAfter) : COPY.interval(retryAfter),
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
