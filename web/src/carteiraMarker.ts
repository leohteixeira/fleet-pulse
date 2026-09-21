import { CAR_BODY, CAR_GLASS, escapeAttr, escapeHtml } from './carIcon';
import { lateColor, type BookRow, type VehicleState } from './carteiraState';

export function leasingMarkerHtml(row: BookRow, selected: boolean): string {
  const state = row.vehicleState;
  const offline = !row.online;
  const heading = row.vehicle?.heading ?? 0;
  const fill = fillFor(state, offline);
  const stroke = strokeFor(state, offline);
  const dash = dashFor(state, offline);
  const win = winFor(state, offline);
  const op = offline ? 0.8 : 1;
  const band = lateColor(row.overdueBand);
  const chipText = row.daysLate > 0 ? `${row.displayId} ${row.daysLate}d` : row.displayId;
  const blocked = state === 'bloqueado' && !selected;
  const chipBg = selected ? 'var(--accent)' : blocked ? 'var(--err)' : 'var(--chip)';
  const chipFg = selected || blocked ? '#fff' : row.daysLate > 0 ? band : 'var(--text-2)';
  const chipBd = selected
    ? 'var(--accent)'
    : blocked
      ? 'var(--err)'
      : state === 'armado'
        ? 'rgba(245,158,11,.6)'
        : 'var(--chip-border)';
  const armed = state === 'armado' ? '<span class="fp-armed marker-armed"></span>' : '';
  const ring = selected ? '<span class="marker-ring"></span>' : '';
  return `<div class="marker marker-lease" data-state="${state}" data-vin="${escapeAttr(row.vin)}" style="opacity:${op}">${ring}${armed}<svg width="14" height="24" viewBox="0 0 12 22" style="display:block;transform:rotate(${heading}deg);transform-origin:50% 50%;filter:drop-shadow(0 1px 1px rgba(0,0,0,.8))"><path d="${CAR_BODY}" fill="${fill}" stroke="${stroke}" stroke-width="1"${dash ? ` stroke-dasharray="${dash}"` : ''}/><path d="${CAR_GLASS}" fill="${win}"/></svg><span class="marker-chip" style="color:${chipFg};background:${chipBg};border-color:${chipBd}">${escapeHtml(chipText)}</span></div>`;
}

function fillFor(state: VehicleState, offline: boolean): string {
  if (state === 'bloqueado') {
    return 'var(--err)';
  }
  if (state === 'armado') {
    return 'var(--warn)';
  }
  if (state === 'desbloqueio_pendente' || offline) {
    return 'var(--v-offline)';
  }
  return 'var(--v-in-use)';
}

function strokeFor(state: VehicleState, offline: boolean): string {
  if (state === 'desbloqueio_pendente') {
    return 'var(--err)';
  }
  if (offline) {
    return 'var(--text-3)';
  }
  return 'rgba(0,0,0,.6)';
}

function dashFor(state: VehicleState, offline: boolean): string {
  if (state === 'desbloqueio_pendente' || offline) {
    return '2 1.5';
  }
  return '';
}

function winFor(state: VehicleState, offline: boolean): string {
  if (state === 'desbloqueio_pendente') {
    return 'rgba(239,68,68,.35)';
  }
  if (offline) {
    return 'rgba(91,100,114,.5)';
  }
  return 'var(--glass)';
}
