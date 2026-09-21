export const OFFLINE_MS = 60_000;

export type PresentationState = 'disponivel' | 'em_uso' | 'fora' | 'offline';

export const PRESENTATION_LABELS: Record<PresentationState, string> = {
  disponivel: 'disponível',
  em_uso: 'em uso',
  fora: 'fora',
  offline: 'offline',
};

export type Polygon = {
  south: number;
  north: number;
  west: number;
  east: number;
};

export type Vehicle = {
  vin: string;
  displayId: string;
  lat: number;
  lng: number;
  plate: string;
  model: string;
  battery: number;
  speed: number;
  heading: number;
  ignition: boolean;
  locked: boolean;
  odometer: number;
  trip: number;
  lastSeen: number | null;
};

export type Snapshot = {
  polygon: Polygon;
  vehicles: SnapshotVehicle[];
};

export type SnapshotVehicle = {
  vin: string;
  displayId?: string;
  lat?: number;
  lng?: number;
  plate?: string;
  model?: string;
  battery?: number;
  speed?: number;
  heading?: number;
  ignition?: boolean;
  locked?: boolean;
  odometer?: number;
  trip?: number;
};

export type CommandAction = 'lock' | 'unlock';
export type CommandState = 'PENDING' | 'SENT' | 'ACKED' | 'FAILED' | 'TIMEOUT';

export type Command = {
  id: string;
  vin: string;
  action: CommandAction;
  state: CommandState;
};

export type FeedKind = 'enviado' | 'confirmado' | 'falhou' | 'expirou' | 'fora';

export type FeedEvent = {
  id: string;
  at: number;
  vin: string;
  displayId: string;
  kind: FeedKind;
  description: string;
};

export const TIMEOUT_COPY = 'Sem resposta em 5 segundos';

export type FleetState = {
  polygon: Polygon | null;
  vehicles: Record<string, Vehicle>;
  selectedVin: string | null;
  commands: Record<string, Command[]>;
  feed: FeedEvent[];
  feedOpen: boolean;
};

export type Action =
  | { type: 'hydrate'; snapshot: Snapshot }
  | { type: 'patch'; payload: unknown; now?: number }
  | { type: 'command'; payload: unknown; now?: number }
  | { type: 'area-exit'; payload: unknown; now?: number }
  | { type: 'select'; vin: string | null }
  | { type: 'toggle-feed' };

export const initialState: FleetState = {
  polygon: null,
  vehicles: {},
  selectedVin: null,
  commands: {},
  feed: [],
  feedOpen: true,
};

export function headingBucket(heading: number): number {
  return Math.floor(heading / 10) * 10;
}

export function iconIdentity(
  presentation: PresentationState,
  selected: boolean,
  heading: number,
): string {
  return `${presentation}|${selected}|${headingBucket(heading)}`;
}

export type MarkerPlan = 'create' | 'move' | 'rebuild';

export function planMarkerUpdate(
  existingKey: string | undefined,
  identity: string,
): MarkerPlan {
  if (existingKey === undefined) {
    return 'create';
  }
  return existingKey === identity ? 'move' : 'rebuild';
}

export function contains(polygon: Polygon, lat: number, lng: number): boolean {
  return lat >= polygon.south && lat <= polygon.north && lng >= polygon.west && lng <= polygon.east;
}

export function deriveState(
  vehicle: Pick<Vehicle, 'lat' | 'lng' | 'speed' | 'ignition' | 'locked' | 'lastSeen'>,
  polygon: Polygon | null,
  now: number,
): PresentationState {
  if (vehicle.lastSeen == null || now - vehicle.lastSeen > OFFLINE_MS) {
    return 'offline';
  }
  if (!polygon || !contains(polygon, vehicle.lat, vehicle.lng)) {
    return 'fora';
  }
  const parked = vehicle.speed <= 0;
  if (parked && vehicle.locked && !vehicle.ignition) {
    return 'disponivel';
  }
  return 'em_uso';
}

export function reducer(state: FleetState, action: Action): FleetState {
  switch (action.type) {
    case 'hydrate':
      return hydrate(action.snapshot);
    case 'patch':
      return patch(state, action.payload, action.now ?? Date.now());
    case 'command':
      return applyCommand(state, action.payload, action.now ?? Date.now());
    case 'area-exit':
      return applyAreaExit(state, action.payload, action.now ?? Date.now());
    case 'select':
      return { ...state, selectedVin: action.vin };
    case 'toggle-feed':
      return { ...state, feedOpen: !state.feedOpen };
    default:
      return state;
  }
}

function hydrate(snapshot: Snapshot): FleetState {
  const vehicles: Record<string, Vehicle> = {};
  for (const raw of snapshot.vehicles) {
    if (!raw.vin) {
      continue;
    }
    vehicles[raw.vin] = vehicleFromSnapshot(raw);
  }
  return {
    polygon: snapshot.polygon,
    vehicles,
    selectedVin: null,
    commands: {},
    feed: [],
    feedOpen: true,
  };
}

function patch(state: FleetState, payload: unknown, now: number): FleetState {
  const parsed = parsePatch(payload);
  if (!parsed) {
    return state;
  }
  const current = state.vehicles[parsed.vin];
  if (!current) {
    return state;
  }
  return {
    ...state,
    vehicles: {
      ...state.vehicles,
      [parsed.vin]: applyPatch(current, parsed, now),
    },
  };
}

function vehicleFromSnapshot(raw: SnapshotVehicle): Vehicle {
  return {
    vin: raw.vin,
    displayId: raw.displayId ?? '',
    lat: raw.lat ?? 0,
    lng: raw.lng ?? 0,
    plate: raw.plate ?? '',
    model: raw.model ?? '',
    battery: raw.battery ?? 0,
    speed: raw.speed ?? 0,
    heading: raw.heading ?? 0,
    ignition: raw.ignition ?? false,
    locked: raw.locked ?? false,
    odometer: raw.odometer ?? 0,
    trip: raw.trip ?? 0,
    lastSeen: null,
  };
}

type PatchFields = {
  vin: string;
  displayId?: string;
  lat?: number;
  lng?: number;
  plate?: string;
  model?: string;
  battery?: number;
  speed?: number;
  heading?: number;
  ignition?: boolean;
  locked?: boolean;
  odometer?: number;
  trip?: number;
};

function parsePatch(payload: unknown): PatchFields | null {
  const value = typeof payload === 'string' ? parseJson(payload) : payload;
  if (!isRecord(value) || typeof value.vin !== 'string' || value.vin === '') {
    return null;
  }
  return value as PatchFields;
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

function applyPatch(current: Vehicle, next: PatchFields, now: number): Vehicle {
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

export function isActiveState(state: CommandState): boolean {
  return state === 'PENDING' || state === 'SENT';
}

export function vehicleCommands(list: Command[] | undefined): {
  inFlight: Command | null;
  queued: Command | null;
} {
  const commands = list ?? [];
  const sent = commands.find((cmd) => cmd.state === 'SENT');
  const pending = commands.filter((cmd) => cmd.state === 'PENDING');
  if (sent) {
    return { inFlight: sent, queued: pending[0] ?? null };
  }
  if (pending.length > 0) {
    return { inFlight: pending[0] ?? null, queued: pending[1] ?? null };
  }
  const last = commands.at(-1);
  return { inFlight: last ?? null, queued: null };
}

export type CommandButtons = {
  unlockDisabled: boolean;
  lockDisabled: boolean;
  unlockLabel: string;
  lockLabel: string;
};

export function commandButtons(list: Command[] | undefined): CommandButtons {
  const { inFlight, queued } = vehicleCommands(list);
  const busy = !!inFlight && isActiveState(inFlight.state);
  const slotFull = !!queued && isActiveState(queued.state);
  if (slotFull) {
    return {
      unlockDisabled: true,
      lockDisabled: true,
      unlockLabel: unlockLabel(inFlight),
      lockLabel: lockLabel(inFlight),
    };
  }
  if (!busy || !inFlight) {
    return { unlockDisabled: false, lockDisabled: false, unlockLabel: 'Destravar', lockLabel: 'Travar' };
  }
  return {
    unlockDisabled: inFlight.action === 'unlock',
    lockDisabled: inFlight.action === 'lock',
    unlockLabel: unlockLabel(inFlight),
    lockLabel: lockLabel(inFlight),
  };
}

function unlockLabel(cmd: Command | null): string {
  if (!cmd || cmd.action !== 'unlock' || !isActiveState(cmd.state)) {
    return 'Destravar';
  }
  return cmd.state === 'PENDING' ? 'Enviando…' : 'Aguardando…';
}

function lockLabel(cmd: Command | null): string {
  if (!cmd || cmd.action !== 'lock' || !isActiveState(cmd.state)) {
    return 'Travar';
  }
  return cmd.state === 'PENDING' ? 'Enviando…' : 'Aguardando…';
}

export type CommandLine = {
  label: string;
  tone: 'idle' | 'accent' | 'ok' | 'err' | 'warn';
};

export function commandLine(cmd: Command | null): CommandLine {
  if (!cmd) {
    return { label: 'OCIOSO', tone: 'idle' };
  }
  switch (cmd.state) {
    case 'PENDING':
      return { label: 'PENDENTE', tone: 'accent' };
    case 'SENT':
      return { label: 'ENVIADO · aguardando ACK', tone: 'accent' };
    case 'ACKED':
      return { label: 'CONFIRMADO', tone: 'ok' };
    case 'FAILED':
      return { label: 'FALHOU', tone: 'err' };
    case 'TIMEOUT':
      return { label: 'EXPIROU', tone: 'warn' };
    default:
      return { label: 'OCIOSO', tone: 'idle' };
  }
}

export type HeaderKpis = {
  vehicles: number;
  disponiveis: number;
  emUso: number;
  fora: number;
  offline: number;
  pendentes: number;
};

export function headerKpis(
  vehicles: Vehicle[],
  polygon: Polygon | null,
  now: number,
  commands: Record<string, Command[]>,
): HeaderKpis {
  const counts: HeaderKpis = {
    vehicles: vehicles.length,
    disponiveis: 0,
    emUso: 0,
    fora: 0,
    offline: 0,
    pendentes: 0,
  };
  for (const vehicle of vehicles) {
    switch (deriveState(vehicle, polygon, now)) {
      case 'disponivel':
        counts.disponiveis += 1;
        break;
      case 'em_uso':
        counts.emUso += 1;
        break;
      case 'fora':
        counts.fora += 1;
        break;
      case 'offline':
        counts.offline += 1;
        break;
    }
  }
  for (const list of Object.values(commands)) {
    for (const cmd of list) {
      if (isActiveState(cmd.state)) {
        counts.pendentes += 1;
      }
    }
  }
  return counts;
}

function applyCommand(state: FleetState, payload: unknown, now: number): FleetState {
  const rec = parseCommand(payload);
  if (!rec) {
    return state;
  }
  const current = state.commands[rec.vin] ?? [];
  const idx = current.findIndex((cmd) => cmd.id === rec.id);
  const prev = idx >= 0 ? current[idx] : undefined;
  if (prev && commandRank(prev.state) > commandRank(rec.state)) {
    return state;
  }
  if (prev && prev.state === rec.state && prev.action === rec.action) {
    return state;
  }
  const nextList = idx >= 0 ? current.map((cmd) => (cmd.id === rec.id ? rec : cmd)) : [...current, rec];
  const event = feedFromCommand(rec, prev, state.vehicles[rec.vin], now);
  return {
    ...state,
    commands: { ...state.commands, [rec.vin]: nextList },
    feed: event ? [event, ...state.feed] : state.feed,
  };
}

function applyAreaExit(state: FleetState, payload: unknown, now: number): FleetState {
  const value = typeof payload === 'string' ? parseJson(payload) : payload;
  if (!isRecord(value) || typeof value.vin !== 'string' || value.vin === '') {
    return state;
  }
  const vin = value.vin;
  const vehicle = state.vehicles[vin];
  const displayId =
    (typeof value.displayId === 'string' && value.displayId) || vehicle?.displayId || vin;
  const event: FeedEvent = {
    id: `area-exit-${vin}-${now}`,
    at: now,
    vin,
    displayId,
    kind: 'fora',
    description: 'Saiu da área permitida Centro',
  };
  return { ...state, feed: [event, ...state.feed] };
}

function commandRank(state: CommandState): number {
  switch (state) {
    case 'PENDING':
      return 0;
    case 'SENT':
      return 1;
    case 'ACKED':
    case 'FAILED':
    case 'TIMEOUT':
      return 2;
    default:
      return 0;
  }
}

function parseCommand(payload: unknown): Command | null {
  const value = typeof payload === 'string' ? parseJson(payload) : payload;
  if (!isRecord(value) || typeof value.id !== 'string' || value.id === '') {
    return null;
  }
  if (typeof value.vin !== 'string' || value.vin === '') {
    return null;
  }
  if (value.action !== 'lock' && value.action !== 'unlock') {
    return null;
  }
  if (
    value.state !== 'PENDING' &&
    value.state !== 'SENT' &&
    value.state !== 'ACKED' &&
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

function feedFromCommand(
  rec: Command,
  prev: Command | undefined,
  vehicle: Vehicle | undefined,
  now: number,
): FeedEvent | null {
  if (prev?.state === rec.state) {
    return null;
  }
  const displayId = vehicle?.displayId || rec.vin;
  const actionLabel = rec.action === 'unlock' ? 'Destravar portas' : 'Travar portas';
  switch (rec.state) {
    case 'SENT':
      return {
        id: `${rec.id}-sent`,
        at: now,
        vin: rec.vin,
        displayId,
        kind: 'enviado',
        description: actionLabel,
      };
    case 'ACKED':
      return {
        id: `${rec.id}-acked`,
        at: now,
        vin: rec.vin,
        displayId,
        kind: 'confirmado',
        description: rec.action === 'unlock' ? 'Portas destravadas' : 'Portas travadas',
      };
    case 'FAILED':
      return {
        id: `${rec.id}-failed`,
        at: now,
        vin: rec.vin,
        displayId,
        kind: 'falhou',
        description: 'Veículo recusou o comando',
      };
    case 'TIMEOUT':
      return {
        id: `${rec.id}-timeout`,
        at: now,
        vin: rec.vin,
        displayId,
        kind: 'expirou',
        description: TIMEOUT_COPY,
      };
    default:
      return null;
  }
}
