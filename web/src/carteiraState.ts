import { headingBucket, type Vehicle } from './state';
import { COPY } from './carteiraCopy';

export const GREATER_SP = {
  south: -23.63,
  north: -23.49,
  west: -46.87,
  east: -46.41,
} as const;

export const RENTAL_VIN_PREFIX = 'FPULSESAO';

export type OverdueBand = 'em_dia' | '1_15' | '16_30' | 'acima_30';
export type VehicleState = 'ativo' | 'armado' | 'bloqueado' | 'desbloqueio_pendente';
export type BandFilter = 'all' | OverdueBand;
export type VehicleFilter = 'all' | VehicleState;

export type LeaseCommandState =
  | 'REQUESTED'
  | 'ARMED'
  | 'SENT'
  | 'ACKED'
  | 'CANCELLED'
  | 'FAILED'
  | 'TIMEOUT';

export type LeaseCommand = {
  id: string;
  vin: string;
  action: 'block' | 'unlock';
  state: LeaseCommandState;
};

export type Contract = {
  id: string;
  vin: string;
  clientName: string;
  payerProfile: string;
  daysLate: number;
  overdueBand: OverdueBand;
};

export type Installment = {
  id: string;
  dueOn: string;
  amount: string;
  paid: boolean;
};

export type AuditEntry = {
  id?: number;
  action: string;
  origin: 'VISITANTE' | 'SISTEMA' | string;
  visitorHash?: string;
  createdAt: string;
};

export type ContractDetail = Contract & {
  installments: Installment[];
  audit: AuditEntry[];
};

export type ClockSnapshot = {
  simulated: string;
  real: string;
  rate: string;
  multiplier: number;
};

export type BookRow = Contract & {
  vehicle: Vehicle | null;
  displayId: string;
  plate: string;
  model: string;
  vehicleState: VehicleState;
  command: LeaseCommand | null;
  online: boolean;
  lastSeen: number | null;
};

export const BAND_LABEL: Record<OverdueBand, string> = {
  em_dia: COPY.bandOk,
  '1_15': COPY.band115,
  '16_30': COPY.band1630,
  acima_30: COPY.band30,
};

export const VS_LABEL: Record<VehicleState, string> = {
  ativo: COPY.vsAtivo,
  armado: COPY.vsArmado,
  bloqueado: COPY.vsBloqueado,
  desbloqueio_pendente: COPY.vsUnlock,
};

export function isLeasingVin(vin: string): boolean {
  return vin !== '' && !vin.startsWith(RENTAL_VIN_PREFIX);
}

export function bandOf(daysLate: number): OverdueBand {
  if (daysLate <= 0) {
    return 'em_dia';
  }
  if (daysLate <= 15) {
    return '1_15';
  }
  if (daysLate <= 30) {
    return '16_30';
  }
  return 'acima_30';
}

export function lateSegments(band: OverdueBand): 0 | 1 | 2 | 3 {
  switch (band) {
    case 'em_dia':
      return 0;
    case '1_15':
      return 1;
    case '16_30':
      return 2;
    case 'acima_30':
      return 3;
  }
}

export function lateColor(band: OverdueBand): string {
  switch (band) {
    case 'em_dia':
      return 'var(--text-3)';
    case '1_15':
      return 'var(--late-1)';
    case '16_30':
      return 'var(--late-2)';
    case 'acima_30':
      return 'var(--late-3)';
  }
}

export function deriveVehicleState(
  vehicle: Vehicle | null,
  command: LeaseCommand | null,
  now: number,
): VehicleState {
  if (command) {
    if (command.action === 'unlock' && (command.state === 'REQUESTED' || command.state === 'SENT')) {
      return 'desbloqueio_pendente';
    }
    if (command.action === 'block') {
      if (command.state === 'REQUESTED' || command.state === 'ARMED' || command.state === 'SENT') {
        return 'armado';
      }
      if (command.state === 'ACKED') {
        return 'bloqueado';
      }
    }
  }
  if (vehicle?.locked) {
    return 'bloqueado';
  }
  if (vehicle && vehicle.lastSeen != null && now - vehicle.lastSeen > 60_000 && vehicle.locked) {
    return 'bloqueado';
  }
  return 'ativo';
}

export function isOnline(vehicle: Vehicle | null, now: number): boolean {
  if (!vehicle || vehicle.lastSeen == null) {
    return false;
  }
  return now - vehicle.lastSeen <= 60_000;
}

export function mergeBook(
  contracts: Contract[],
  vehicles: Vehicle[],
  commands: Record<string, LeaseCommand>,
  now: number,
): BookRow[] {
  const byVin = new Map<string, Vehicle>();
  for (const vehicle of vehicles) {
    if (!isLeasingVin(vehicle.vin)) {
      continue;
    }
    byVin.set(vehicle.vin, vehicle);
  }
  const rows: BookRow[] = [];
  for (const contract of contracts) {
    if (!isLeasingVin(contract.vin)) {
      continue;
    }
    const vehicle = byVin.get(contract.vin) ?? null;
    const command = commands[contract.vin] ?? null;
    rows.push({
      ...contract,
      vehicle,
      displayId: vehicle?.displayId || contract.id.slice(0, 4),
      plate: vehicle?.plate ?? '',
      model: vehicle?.model ?? '',
      vehicleState: deriveVehicleState(vehicle, command, now),
      command,
      online: isOnline(vehicle, now),
      lastSeen: vehicle?.lastSeen ?? null,
    });
  }
  return sortRows(rows);
}

export function sortRows(rows: BookRow[]): BookRow[] {
  return [...rows].sort((a, b) => {
    if (a.daysLate !== b.daysLate) {
      return b.daysLate - a.daysLate;
    }
    const byName = a.clientName.localeCompare(b.clientName, 'pt-BR');
    if (byName !== 0) {
      return byName;
    }
    return a.vin.localeCompare(b.vin);
  });
}

export function filterRows(
  rows: BookRow[],
  band: BandFilter,
  vehicleState: VehicleFilter,
  query: string,
): BookRow[] {
  const q = query.trim().toLowerCase();
  return rows.filter((row) => {
    if (band !== 'all' && row.overdueBand !== band) {
      return false;
    }
    if (vehicleState !== 'all' && row.vehicleState !== vehicleState) {
      return false;
    }
    if (!q) {
      return true;
    }
    return (
      row.plate.toLowerCase().includes(q) ||
      row.clientName.toLowerCase().includes(q) ||
      row.id.toLowerCase().includes(q) ||
      row.displayId.toLowerCase().includes(q)
    );
  });
}

export type CarteiraKpis = {
  active: number;
  late: number;
  overdueAmount: number;
  overdueInstallments: number;
  armed: number;
  blocked: number;
  unlockPending: number;
};

export function carteiraKpis(rows: BookRow[], details: Record<string, ContractDetail>): CarteiraKpis {
  const kpis: CarteiraKpis = {
    active: rows.length,
    late: 0,
    overdueAmount: 0,
    overdueInstallments: 0,
    armed: 0,
    blocked: 0,
    unlockPending: 0,
  };
  for (const row of rows) {
    if (row.daysLate > 0) {
      kpis.late += 1;
    }
    switch (row.vehicleState) {
      case 'armado':
        kpis.armed += 1;
        break;
      case 'bloqueado':
        kpis.blocked += 1;
        break;
      case 'desbloqueio_pendente':
        kpis.unlockPending += 1;
        break;
      default:
        break;
    }
    const detail = details[row.id];
    if (!detail) {
      continue;
    }
    const { amount, count } = overdueFromInstallments(detail.installments, row.daysLate);
    kpis.overdueAmount += amount;
    kpis.overdueInstallments += count;
  }
  return kpis;
}

export function overdueFromInstallments(
  installments: Installment[],
  daysLate: number,
): { amount: number; count: number } {
  if (daysLate <= 0) {
    return { amount: 0, count: 0 };
  }
  let amount = 0;
  let count = 0;
  for (const inst of installments) {
    if (inst.paid) {
      continue;
    }
    const value = Number(inst.amount);
    if (Number.isFinite(value)) {
      amount += value;
    }
    count += 1;
    if (count >= Math.max(1, Math.ceil(daysLate / 30))) {
      break;
    }
  }
  return { amount, count };
}

export function nextDue(installments: Installment[]): Installment | null {
  return installments.find((inst) => !inst.paid) ?? null;
}

export function installmentAmount(installments: Installment[]): string {
  return installments[0]?.amount ?? '';
}

export function moneyBRL(raw: string | number): string {
  const n = typeof raw === 'number' ? raw : Number(raw);
  if (!Number.isFinite(n)) {
    return typeof raw === 'string' && raw ? raw : '—';
  }
  return n.toLocaleString('pt-BR', { style: 'currency', currency: 'BRL' });
}

export function formatDate(iso: string): string {
  const day = iso.slice(0, 10);
  const [y, m, d] = day.split('-');
  if (!y || !m || !d) {
    return iso;
  }
  return `${d}/${m}/${y}`;
}

export function formatDateTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  const p = (n: number) => String(n).padStart(2, '0');
  return `${p(date.getUTCDate())}/${p(date.getUTCMonth() + 1)}/${date.getUTCFullYear()} ${p(date.getUTCHours())}:${p(date.getUTCMinutes())}`;
}

const WEEKDAYS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'] as const;

export function formatSimChip(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  const p = (n: number) => String(n).padStart(2, '0');
  const wd = WEEKDAYS[date.getUTCDay()] ?? 'dom';
  return `${wd} ${p(date.getUTCDate())}/${p(date.getUTCMonth() + 1)}/${date.getUTCFullYear()} ${p(date.getUTCHours())}:${p(date.getUTCMinutes())}`;
}

export function formatRealTooltip(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return iso;
  }
  return `Data real: ${date.toLocaleString('pt-BR')}`;
}

export function relativeSignal(ageMs: number | null): string {
  if (ageMs == null) {
    return 'offline';
  }
  const s = Math.max(0, Math.round(ageMs / 1000));
  if (s < 60) {
    return `offline ${s}s`;
  }
  const m = Math.floor(s / 60);
  if (m < 60) {
    return `offline ${m}min`;
  }
  return `offline ${Math.floor(m / 60)}h`;
}

export function relativeDays(fromIso: string, simIso: string): string {
  const from = new Date(fromIso).getTime();
  const sim = new Date(simIso).getTime();
  if (!Number.isFinite(from) || !Number.isFinite(sim)) {
    return COPY.dialogNever;
  }
  const days = Math.floor(Math.max(0, sim - from) / 86_400_000);
  if (days >= 1) {
    return `há ${days} dia${days > 1 ? 's' : ''}`;
  }
  const hours = Math.floor(Math.max(0, sim - from) / 3_600_000);
  return `há ${hours}h`;
}

export function lastNotifyAt(detail: ContractDetail | null): string | null {
  if (!detail) {
    return null;
  }
  for (let i = detail.audit.length - 1; i >= 0; i -= 1) {
    const row = detail.audit[i];
    if (row?.action === 'notify') {
      return row.createdAt;
    }
  }
  return null;
}

export function installmentWindow(installments: Installment[], simDay: string): Installment[] {
  const paid = installments.filter((inst) => inst.paid).length;
  const overdue = installments.filter((inst) => !inst.paid && inst.dueOn <= simDay).length;
  const start = Math.max(0, paid - 2);
  const end = Math.min(installments.length, paid + overdue + 2);
  return installments.slice(start, Math.max(start, end));
}

export function installmentStatus(inst: Installment, simDay: string): 'paga' | 'vencida' | 'aberta' {
  if (inst.paid) {
    return 'paga';
  }
  return inst.dueOn <= simDay ? 'vencida' : 'aberta';
}

export function pendingLabel(command: LeaseCommand | null): string | null {
  if (!command) {
    return null;
  }
  if (command.action === 'unlock' && (command.state === 'REQUESTED' || command.state === 'SENT')) {
    return COPY.pendingUnlock;
  }
  if (command.action !== 'block') {
    return null;
  }
  switch (command.state) {
    case 'REQUESTED':
      return COPY.pendingRequested;
    case 'ARMED':
      return COPY.pendingArmed;
    case 'SENT':
      return COPY.pendingSent;
    default:
      return null;
  }
}

export function leasingIconIdentity(
  state: VehicleState,
  selected: boolean,
  online: boolean,
  daysLate: number,
  heading: number,
): string {
  return `${state}|${selected}|${online}|${daysLate}|${headingBucket(heading)}`;
}

export function parseLeaseCommand(payload: unknown): LeaseCommand | null {
  const value = typeof payload === 'string' ? parseJson(payload) : payload;
  if (!isRecord(value) || typeof value.id !== 'string' || value.id === '') {
    return null;
  }
  if (typeof value.vin !== 'string' || value.vin === '' || !isLeasingVin(value.vin)) {
    return null;
  }
  if (value.action !== 'block' && value.action !== 'unlock') {
    return null;
  }
  if (
    value.state !== 'REQUESTED' &&
    value.state !== 'ARMED' &&
    value.state !== 'SENT' &&
    value.state !== 'ACKED' &&
    value.state !== 'CANCELLED' &&
    value.state !== 'FAILED' &&
    value.state !== 'TIMEOUT'
  ) {
    return null;
  }
  return {
    id: value.id,
    vin: value.vin,
    action: value.action,
    state: value.state,
  };
}

export function parseLeasePatch(payload: unknown): Partial<Vehicle> & { vin: string } | null {
  const value = typeof payload === 'string' ? parseJson(payload) : payload;
  if (!isRecord(value) || typeof value.vin !== 'string' || value.vin === '') {
    return null;
  }
  if (!isLeasingVin(value.vin)) {
    return null;
  }
  return value as Partial<Vehicle> & { vin: string };
}

export function applyVehiclePatch(current: Vehicle, next: Partial<Vehicle>, now: number): Vehicle {
  return {
    ...current,
    displayId: next.displayId ? next.displayId : current.displayId,
    lat: next.lat ?? current.lat,
    lng: next.lng ?? current.lng,
    plate: next.plate ?? current.plate,
    model: next.model ?? current.model,
    battery: next.battery ?? current.battery,
    speed: next.speed ?? current.speed,
    heading: next.heading ?? current.heading,
    ignition: next.ignition ?? current.ignition,
    locked: next.locked ?? current.locked,
    odometer: next.odometer ?? current.odometer,
    trip: next.trip ?? current.trip,
    lastSeen: now,
  };
}

export function attentionCount(rows: BookRow[]): number {
  return rows.filter((row) => row.daysLate >= 16 && row.vehicleState === 'ativo').length;
}

export function auditTone(action: string): 'err' | 'warn' | 'ok' | 'accent' | 'idle' {
  const key = action.toLowerCase();
  if (key.includes('fail') || key.includes('overdue') || key === 'block') {
    return key === 'block' ? 'warn' : 'err';
  }
  if (key.includes('timeout') || key.includes('expir')) {
    return 'warn';
  }
  if (key.includes('payment') || key.includes('unlock') || key === 'cancel') {
    return 'ok';
  }
  if (key === 'notify') {
    return 'accent';
  }
  return 'idle';
}

export function auditActionLabel(action: string): string {
  switch (action) {
    case 'notify':
      return 'Notificação enviada';
    case 'block':
      return 'Bloqueio solicitado';
    case 'cancel':
      return 'Bloqueio cancelado';
    case 'payment':
      return 'Pagamento registrado';
    default:
      return action;
  }
}

function parseJson(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
