import { COPY, commandLineCopy, type WriteRefusal } from './carteiraCopy';
import {
  attentionCount,
  auditActionLabel,
  auditTone,
  formatDate,
  formatDateTime,
  installmentStatus,
  installmentWindow,
  lastNotifyAt,
  lateColor,
  lateSegments,
  moneyBRL,
  overdueFromInstallments,
  VS_LABEL,
  type BookRow,
  type ClockSnapshot,
  type ContractDetail,
} from './carteiraState';

export type ActionKey = 'notify' | 'block' | 'cancel' | 'pay';

type CarteiraPanelProps = {
  row: BookRow | null;
  detail: ContractDetail | null;
  rows: BookRow[];
  clock: ClockSnapshot | null;
  now: number;
  refusals: Partial<Record<ActionKey, WriteRefusal>>;
  rate: WriteRefusal | null;
  interval: WriteRefusal | null;
  busy: ActionKey | null;
  onClose: () => void;
  onNotify: () => void;
  onBlock: () => void;
  onCancel: () => void;
  onPay: () => void;
};

export function CarteiraPanel({
  row,
  detail,
  rows,
  clock,
  now,
  refusals,
  rate,
  interval,
  busy,
  onClose,
  onNotify,
  onBlock,
  onCancel,
  onPay,
}: CarteiraPanelProps) {
  if (!row) {
    return (
      <aside className="panel ct-panel" aria-label="Painel do contrato">
        <div className="panel-empty">
          <div className="crosshair" aria-hidden="true">
            <span />
            <span />
          </div>
          <p>{COPY.panelEmpty}</p>
          <p className="panel-empty-hint">{COPY.panelEmptyHint}</p>
          <p className="panel-empty-count">{COPY.panelEmptyCount(attentionCount(rows))}</p>
        </div>
      </aside>
    );
  }

  const simDay = clock?.simulated.slice(0, 10) ?? '';
  const installments = detail?.installments ?? [];
  const hist = installmentWindow(installments, simDay);
  const overdue = overdueFromInstallments(installments, row.daysLate);
  const paid = installments.filter((inst) => inst.paid).length;
  const total = installments.length || 1;
  const next = installments.find((inst) => !inst.paid);
  const instAmount = installments[0]?.amount ?? '';
  const band = lateColor(row.overdueBand);
  const segs = lateSegments(row.overdueBand);
  const vehicle = row.vehicle;
  const immobilized = row.vehicleState === 'bloqueado';
  const moving = !!vehicle && vehicle.speed > 0 && !immobilized;
  const notifyAt = lastNotifyAt(detail);
  const cmd = commandLineCopy(row.command?.state ?? null, row.command?.action);
  const sending = row.command?.action === 'block' && (row.command.state === 'REQUESTED' || row.command.state === 'SENT');
  const armed = row.vehicleState === 'armado';

  const actions: {
    key: ActionKey;
    label: string;
    kind: 'primary' | 'danger' | 'secondary';
    hint: string;
    spin: boolean;
    run: () => void;
  }[] = [
    {
      key: 'notify',
      label: COPY.notify,
      kind: 'primary',
      hint: notifyAt ? COPY.lastNotify(formatDateTime(notifyAt)) : COPY.idleNotify,
      spin: false,
      run: onNotify,
    },
    {
      key: 'block',
      label: sending ? COPY.sending : armed ? COPY.blockArmed : COPY.block,
      kind: 'danger',
      hint: sending ? '' : armed ? COPY.blockArmedHint : COPY.idleBlock,
      spin: sending,
      run: onBlock,
    },
    {
      key: 'cancel',
      label: COPY.cancel,
      kind: 'secondary',
      hint: COPY.idleCancel,
      spin: false,
      run: onCancel,
    },
    {
      key: 'pay',
      label: COPY.pay,
      kind: 'secondary',
      hint: COPY.idlePay,
      spin: false,
      run: onPay,
    },
  ];

  return (
    <aside className="panel ct-panel" aria-label="Painel do contrato">
      <div className="panel-scroll">
        <div className="panel-head">
          <div className="panel-head-main">
            <div className="panel-plate-row">
              <span className="ct-client-name">{row.clientName}</span>
              <span className="panel-id">{row.displayId}</span>
            </div>
            <div className="panel-model">
              <span className="mono">{row.plate}</span> {row.model}
            </div>
          </div>
          <span className={`vs-pill vs-${row.vehicleState}`}>
            <span className={`vs-dot${row.vehicleState === 'armado' ? ' fp-armed' : ''}`} />
            {VS_LABEL[row.vehicleState]}
          </span>
          <button type="button" className="panel-close" onClick={onClose} title="Fechar">
            ×
          </button>
        </div>

        <div className="ct-facts">
          <div className="metric">
            <div className="metric-label">{COPY.factOverdue}</div>
            <div className={`metric-value mono${overdue.amount > 0 ? ' tone-err' : ''}`}>
              {moneyBRL(overdue.amount)}
            </div>
            <div className="ct-late" style={{ color: band }}>
              <span className="late-segs">
                <i style={{ background: segs >= 1 ? band : 'var(--line)' }} />
                <i style={{ background: segs >= 2 ? band : 'var(--line)' }} />
                <i style={{ background: segs >= 3 ? band : 'var(--line)' }} />
              </span>
              {row.daysLate > 0 ? `${row.daysLate} dias · ${row.overdueBand === 'acima_30' ? COPY.band30 : row.overdueBand === '16_30' ? COPY.band1630 : COPY.band115}` : COPY.onTime}
            </div>
          </div>
          <div className="metric">
            <div className="metric-label">{COPY.factInst}</div>
            <div className="metric-value mono">{instAmount ? moneyBRL(instAmount) : '—'}</div>
            <div className="metric-sub">{COPY.factNext(next ? formatDate(next.dueOn) : '—')}</div>
          </div>
          <div className="ct-paid">
            <div className="ct-paid-lab">
              <span>{COPY.factPaid}</span>
              <span className="mono">
                {paid} / {installments.length}
              </span>
            </div>
            <div className="ct-paid-bar">
              <span style={{ width: `${(paid / total) * 100}%` }} className="is-ok" />
              <span style={{ width: `${(overdue.count / total) * 100}%` }} className="is-err" />
            </div>
          </div>
        </div>

        <div className="ct-section">{COPY.vehicleState}</div>
        <dl className="panel-rows">
          <div className="panel-row">
            <dt>{COPY.motion}</dt>
            <dd>
              <span className={`row-dot ${immobilized ? 'tone-err' : moving ? 'tone-warn' : 'tone-idle'}`} />
              {immobilized
                ? COPY.immobilized
                : moving
                  ? COPY.moving(vehicle?.speed ?? 0)
                  : COPY.stopped}
            </dd>
          </div>
          <div className="panel-row">
            <dt>{COPY.ignition}</dt>
            <dd>
              <span className={`row-dot ${vehicle?.ignition ? 'tone-ok' : 'tone-idle'}`} />
              {vehicle?.ignition ? COPY.ignOn : COPY.ignOff}
            </dd>
          </div>
          <div className="panel-row">
            <dt>{COPY.lastPos}</dt>
            <dd className="row-muted">
              {vehicle
                ? `${vehicle.lat.toFixed(5)}, ${vehicle.lng.toFixed(5)}`
                : '—'}
            </dd>
          </div>
          <div className="panel-row">
            <dt>{COPY.lastSignal}</dt>
            <dd className={row.online ? 'tone-ok' : 'tone-warn'}>
              <span className={`row-dot ${row.online ? 'tone-ok' : 'tone-idle'}`} />
              {row.online ? 'ONLINE' : 'OFFLINE'} ·{' '}
              {row.lastSeen == null ? '—' : relAgo(now - row.lastSeen)}
            </dd>
          </div>
        </dl>

        <div className="ct-section">{COPY.hist}</div>
        <div className="ct-hist">
          {hist.map((inst, index) => {
            const status = installmentStatus(inst, simDay);
            const tone = status === 'paga' ? 'ok' : status === 'vencida' ? 'err' : 'idle';
            return (
              <div key={inst.id} className="ct-hist-row">
                <span className="muted">{paid - 2 + index + 1 > 0 ? String(installments.indexOf(inst) + 1).padStart(2, '0') : '—'}</span>
                <span className={status === 'vencida' ? 'tone-err' : ''}>{formatDate(inst.dueOn)}</span>
                <span className="is-right">{moneyBRL(inst.amount)}</span>
                <span className={`hist-st tone-${tone}`}>
                  <i />
                  {status === 'paga' ? COPY.instPaid : status === 'vencida' ? COPY.instDue : COPY.instOpen}
                </span>
              </div>
            );
          })}
        </div>

        <div className="ct-section">{COPY.audit}</div>
        <div className="ct-audit">
          {(detail?.audit ?? []).slice().reverse().map((entry, index) => {
            const tone = auditTone(entry.action);
            return (
              <div key={`${entry.id ?? entry.createdAt}-${index}`} className={`audit-row${index === 0 ? ' fp-row-in' : ''}`}>
                <span className="audit-rail">
                  <i className={`tone-${tone}`} />
                </span>
                <div>
                  <div className="audit-meta">
                    <span className="muted">{formatDateTime(entry.createdAt)}</span>
                    <span className={entry.origin === 'VISITANTE' ? 'tone-accent' : 'muted'}>
                      {entry.origin === 'VISITANTE' ? COPY.originVisitor : COPY.originSystem}
                    </span>
                  </div>
                  <div className={`audit-act tone-${tone}`}>{auditActionLabel(entry.action)}</div>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div className="ct-actions">
        {rate ? (
          <div className="rate-band" role="status">
            <span className="rate-dot" />
            {rate.copy}
          </div>
        ) : null}
        <div className="action-grid">
          {actions.map((action) => {
            const refusal = rate ?? interval ?? refusals[action.key];
            const refused = !!refusal || !!rate;
            const reason = refusal?.copy ?? action.hint;
            const reasonTone = refusal?.kind === 'rate' ? 'err' : refusal?.kind === 'interval' ? 'warn' : 'idle';
            return (
              <div key={action.key} className="action-slot">
                <button
                  type="button"
                  className={`btn-${action.kind}${action.spin ? ' is-pending' : ''}`}
                  disabled={refused || busy === action.key}
                  style={{ opacity: refused ? 0.45 : 1 }}
                  onClick={action.run}
                >
                  {action.spin ? <span className="fp-spin" aria-hidden="true" /> : null}
                  {action.label}
                </button>
                <span className={`action-reason tone-${reasonTone}`}>{reason}</span>
              </div>
            );
          })}
        </div>
        <div className={`cmd-line tone-${cmd.tone}`}>
          <span>{COPY.command}</span>
          <span>
            {cmd.dot ? <span className={`cmd-dot ${cmd.dot === 'armed' ? 'fp-armed' : 'is-live'}`} /> : null}
            {cmd.label}
          </span>
        </div>
      </div>
    </aside>
  );
}

function relAgo(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) {
    return `há ${s}s`;
  }
  const m = Math.floor(s / 60);
  if (m < 60) {
    return `há ${m}min`;
  }
  return `há ${Math.floor(m / 60)}h`;
}
