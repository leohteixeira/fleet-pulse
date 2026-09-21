import { useEffect, useRef } from 'react';

import { COPY } from './carteiraCopy';
import {
  formatDate,
  installmentAmount,
  lateColor,
  lateSegments,
  moneyBRL,
  nextDue,
  pendingLabel,
  relativeSignal,
  VS_LABEL,
  type BookRow,
  type ContractDetail,
} from './carteiraState';

type CarteiraTableProps = {
  rows: BookRow[];
  details: Record<string, ContractDetail>;
  selectedId: string | null;
  scrollId: string | null;
  onScrollDone?: () => void;
  query: string;
  onSelect: (id: string) => void;
  onClear: () => void;
};

export function CarteiraTable({
  rows,
  details,
  selectedId,
  scrollId,
  onScrollDone,
  query,
  onSelect,
  onClear,
}: CarteiraTableProps) {
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!scrollId || !listRef.current) {
      return;
    }
    const index = rows.findIndex((row) => row.id === scrollId);
    if (index < 0) {
      return;
    }
    const y = index * 32;
    const el = listRef.current;
    if (y < el.scrollTop || y + 32 > el.scrollTop + el.clientHeight) {
      el.scrollTop = Math.max(0, y - el.clientHeight / 2 + 16);
    }
    onScrollDone?.();
  }, [onScrollDone, rows, scrollId]);

  return (
    <div className="ct-table">
      <div className="ct-head">
        <span>{COPY.colClient}</span>
        <span>{COPY.colPlate}</span>
        <span className="is-right">{COPY.colInst}</span>
        <span>{COPY.colDue}</span>
        <span>{COPY.colLate}</span>
        <span>{COPY.colVehicle}</span>
        <span>{COPY.colSignal}</span>
      </div>
      <div ref={listRef} className="ct-list">
        {rows.length === 0 ? (
          <div className="ct-empty">
            <div className="ct-empty-box" aria-hidden="true" />
            <p>{COPY.emptyTitle}</p>
            <p className="ct-empty-hint">
              {query.trim() ? COPY.emptySearch(query.trim()) : COPY.emptyHint}
            </p>
            <button type="button" className="btn-secondary ct-clear" onClick={onClear}>
              {COPY.clearFilters}
            </button>
          </div>
        ) : (
          rows.map((row) => {
            const selected = row.id === selectedId;
            const pending = pendingLabel(row.command);
            const detail = details[row.id];
            const inst = detail ? installmentAmount(detail.installments) : '';
            const due = detail ? nextDue(detail.installments) : null;
            const band = lateColor(row.overdueBand);
            const segs = lateSegments(row.overdueBand);
            return (
              <button
                key={row.id}
                type="button"
                className={`ct-row${selected ? ' is-selected' : ''}${pending && !selected ? ' is-pending' : ''}`}
                onClick={() => onSelect(row.id)}
              >
                <span className="ct-client">
                  <span className="mono muted">{row.displayId}</span>
                  <span className="ct-name">{row.clientName}</span>
                </span>
                <span className="ct-plate">
                  <span className="mono">{row.plate}</span>
                  <span className="muted">{row.model}</span>
                </span>
                <span className="mono is-right">{inst ? moneyBRL(inst) : '—'}</span>
                <span
                  className="mono"
                  style={{ color: row.daysLate > 0 ? band : 'var(--text-2)' }}
                >
                  {due ? formatDate(due.dueOn) : '—'}
                </span>
                <span className="ct-late">
                  <span className="late-segs" aria-hidden="true">
                    <i style={{ background: segs >= 1 ? band : 'var(--line)' }} />
                    <i style={{ background: segs >= 2 ? band : 'var(--line)' }} />
                    <i style={{ background: segs >= 3 ? band : 'var(--line)' }} />
                  </span>
                  <span
                    className="mono"
                    style={{
                      color: row.daysLate > 0 ? band : 'var(--text-3)',
                      fontWeight: row.overdueBand === 'acima_30' ? 600 : 500,
                    }}
                  >
                    {row.daysLate > 0 ? `${row.daysLate}d` : COPY.onTime}
                  </span>
                </span>
                <span className="ct-vs">
                  <span className={`vs-pill vs-${row.vehicleState}`}>
                    <span className={`vs-dot${row.vehicleState === 'armado' ? ' fp-armed' : ''}`} />
                    {VS_LABEL[row.vehicleState]}
                  </span>
                  {pending ? (
                    <span className={`pend-label ${row.vehicleState === 'armado' ? 'tone-warn' : 'tone-accent'}`}>
                      {pending}
                    </span>
                  ) : null}
                </span>
                <span className={`ct-sig${row.online ? '' : ' is-off'}`}>
                  <span className={`sig-dot${row.online ? ' is-on' : ''}`} />
                  {row.online
                    ? COPY.online
                    : relativeSignal(row.lastSeen == null ? null : Date.now() - row.lastSeen)}
                </span>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}
