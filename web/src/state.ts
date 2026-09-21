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

export type FleetState = {
  polygon: Polygon | null;
  vehicles: Record<string, Vehicle>;
  selectedVin: string | null;
};

export type Action =
  | { type: 'hydrate'; snapshot: Snapshot }
  | { type: 'patch'; payload: unknown; now?: number }
  | { type: 'select'; vin: string | null };

export const initialState: FleetState = {
  polygon: null,
  vehicles: {},
  selectedVin: null,
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
    case 'select':
      return { ...state, selectedVin: action.vin };
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
