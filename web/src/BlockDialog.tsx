import { COPY, runeCount } from './carteiraCopy';
import {
  formatDateTime,
  moneyBRL,
  relativeDays,
  type BookRow,
  type ClockSnapshot,
} from './carteiraState';

type BlockDialogProps = {
  row: BookRow;
  amount: number;
  lastNotify: string | null;
  clock: ClockSnapshot | null;
  reason: string;
  onReason: (value: string) => void;
  onCancel: () => void;
  onConfirm: () => void;
};

export function BlockDialog({
  row,
  amount,
  lastNotify,
  clock,
  reason,
  onReason,
  onCancel,
  onConfirm,
}: BlockDialogProps) {
  const trimmed = runeCount(reason.trim());
  const can = trimmed >= 8 && trimmed <= 280;
  return (
    <div className="dlg-overlay" onClick={onCancel} role="presentation">
      <div
        className="dlg"
        role="dialog"
        aria-labelledby="dlg-title"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="dlg-head">
          <span className="dlg-square" aria-hidden="true" />
          <span id="dlg-title">{COPY.dialogTitle}</span>
          <span className="mono muted dlg-id">{row.displayId}</span>
        </div>
        <div className="dlg-facts">
          <span>{COPY.dialogClient}</span>
          <span>{row.clientName}</span>
          <span>{COPY.dialogVehicle}</span>
          <span>
            <span className="mono">{row.plate}</span> <span className="muted">{row.model}</span>
          </span>
          <span>{COPY.dialogOverdue}</span>
          <span className="mono tone-err">
            {moneyBRL(amount)} · <strong>{row.daysLate} dias</strong>
          </span>
          <span>{COPY.dialogNotify}</span>
          <span className="mono">
            {lastNotify ? formatDateTime(lastNotify) : COPY.dialogNever}{' '}
            <span className="muted">
              ({lastNotify && clock ? relativeDays(lastNotify, clock.simulated) : COPY.dialogNever})
            </span>
          </span>
        </div>
        <div className="dlg-reason">
          <label>
            {COPY.dialogReason} <span className="tone-err">{COPY.dialogRequired}</span>
          </label>
          <textarea
            rows={3}
            value={reason}
            placeholder={COPY.dialogPlaceholder}
            onChange={(event) => onReason(event.target.value)}
          />
          <span className="dlg-count">{COPY.dialogCounter(trimmed)}</span>
        </div>
        <div className="dlg-notice">
          <span className="dlg-armed" aria-hidden="true" />
          <span>
            <strong className="tone-warn">{COPY.dialogNoticeTitle}</strong> {COPY.dialogNotice}
          </span>
        </div>
        <div className="dlg-actions">
          <button type="button" className="btn-secondary" onClick={onCancel}>
            {COPY.dialogCancel}
          </button>
          <button
            type="button"
            className="btn-danger"
            disabled={!can}
            style={{ opacity: can ? 1 : 0.45 }}
            onClick={onConfirm}
          >
            {COPY.dialogConfirm}
          </button>
        </div>
      </div>
    </div>
  );
}
