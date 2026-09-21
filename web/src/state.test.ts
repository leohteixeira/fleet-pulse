import { describe, expect, it } from 'vitest';

import {
  OFFLINE_MS,
  deriveState,
  headingBucket,
  iconIdentity,
  initialState,
  planMarkerUpdate,
  reducer,
  type Polygon,
  type Snapshot,
  type Vehicle,
} from './state';

const centro: Polygon = {
  south: -23.585,
  north: -23.525,
  west: -46.685,
  east: -46.6,
};

const now = 1_700_000_000_000;

function vehicle(overrides: Partial<Vehicle> = {}): Vehicle {
  return {
    vin: 'FPULSESAO00000001',
    displayId: 'V01',
    lat: -23.55,
    lng: -46.63,
    plate: 'BAL0A01',
    model: 'Fiat Argo',
    battery: 80,
    speed: 0,
    heading: 14,
    ignition: false,
    locked: true,
    odometer: 1000,
    trip: 1,
    lastSeen: now,
    ...overrides,
  };
}

function snapshot(count = 20): Snapshot {
  return {
    polygon: centro,
    vehicles: Array.from({ length: count }, (_, i) => ({
      vin: `FPULSESAO${String(i + 1).padStart(8, '0')}`,
      displayId: `V${String(i + 1).padStart(2, '0')}`,
      lat: -23.55,
      lng: -46.63,
      speed: 18,
      heading: i * 18,
      ignition: i % 3 !== 0,
      locked: i % 3 === 0,
    })),
  };
}

describe('deriveState', () => {
  it('treats missing telemetry as offline so the silent VIN is offline on first paint', () => {
    expect(deriveState(vehicle({ lastSeen: null }), centro, now)).toBe('offline');
  });

  it('treats last telemetry older than 60s as offline', () => {
    expect(deriveState(vehicle({ lastSeen: now - OFFLINE_MS - 1 }), centro, now)).toBe('offline');
  });

  it('derives fora west of Centro or south of Centro', () => {
    expect(deriveState(vehicle({ lng: -46.686 }), centro, now)).toBe('fora');
    expect(deriveState(vehicle({ lat: -23.586 }), centro, now)).toBe('fora');
  });

  it('derives disponível when parked, locked, and ignition off inside the polygon', () => {
    expect(
      deriveState(vehicle({ speed: 0, locked: true, ignition: false }), centro, now),
    ).toBe('disponivel');
  });

  it('derives em uso for the residual non-parked state', () => {
    expect(deriveState(vehicle({ speed: 20 }), centro, now)).toBe('em_uso');
    expect(deriveState(vehicle({ ignition: true }), centro, now)).toBe('em_uso');
    expect(deriveState(vehicle({ locked: false }), centro, now)).toBe('em_uso');
  });
});

describe('planMarkerUpdate', () => {
  it('creates when no marker exists, moves on the same heading bucket, and rebuilds on bucket/state/selection', () => {
    expect(planMarkerUpdate(undefined, iconIdentity('em_uso', false, 14))).toBe('create');
    expect(
      planMarkerUpdate(iconIdentity('em_uso', false, 14), iconIdentity('em_uso', false, 16)),
    ).toBe('move');
    expect(
      planMarkerUpdate(iconIdentity('em_uso', false, 14), iconIdentity('em_uso', false, 25)),
    ).toBe('rebuild');
    expect(
      planMarkerUpdate(iconIdentity('em_uso', false, 14), iconIdentity('fora', false, 14)),
    ).toBe('rebuild');
    expect(
      planMarkerUpdate(iconIdentity('em_uso', false, 14), iconIdentity('em_uso', true, 14)),
    ).toBe('rebuild');
  });
});

describe('headingBucket', () => {
  it('keeps 14° and 16° in the same 10° bucket', () => {
    expect(headingBucket(14)).toBe(10);
    expect(headingBucket(16)).toBe(10);
    expect(iconIdentity('em_uso', false, 14)).toBe(iconIdentity('em_uso', false, 16));
  });

  it('rebuilds the icon identity when heading crosses into 25°', () => {
    expect(headingBucket(25)).toBe(20);
    expect(iconIdentity('em_uso', false, 14)).not.toBe(iconIdentity('em_uso', false, 25));
  });
});

describe('reducer', () => {
  it('hydrates 20 vehicles from the snapshot and marks all as no-telemetry', () => {
    const next = reducer(initialState, { type: 'hydrate', snapshot: snapshot(20) });
    expect(Object.keys(next.vehicles)).toHaveLength(20);
    expect(next.polygon).toEqual(centro);
    const silent = next.vehicles['FPULSESAO00000020'];
    expect(silent?.lastSeen).toBeNull();
    expect(deriveState(silent!, next.polygon, now)).toBe('offline');
    for (const v of Object.values(next.vehicles)) {
      expect(v.lastSeen).toBeNull();
      expect(deriveState(v, next.polygon, now)).toBe('offline');
    }
  });

  it('patches an online VIN and leaves a bad payload unchanged', () => {
    const hydrated = reducer(initialState, { type: 'hydrate', snapshot: snapshot(2) });
    const good = reducer(hydrated, {
      type: 'patch',
      now,
      payload: JSON.stringify({
        vin: 'FPULSESAO00000001',
        lat: -23.54,
        lng: -46.62,
        speed: 0,
        heading: 16,
        ignition: false,
        locked: true,
      }),
    });
    const patched = good.vehicles['FPULSESAO00000001'];
    expect(patched?.lat).toBe(-23.54);
    expect(patched?.lng).toBe(-46.62);
    expect(patched?.lastSeen).toBe(now);
    expect(patched?.heading).toBe(16);
    expect(deriveState(patched!, good.polygon, now)).toBe('disponivel');
    expect(iconIdentity('disponivel', false, 14)).toBe(
      iconIdentity('disponivel', false, patched!.heading),
    );

    const unchanged = reducer(good, { type: 'patch', now: now + 1, payload: '{not-json' });
    expect(unchanged).toBe(good);

    const missingVin = reducer(good, {
      type: 'patch',
      now: now + 1,
      payload: JSON.stringify({ lat: -23.5 }),
    });
    expect(missingVin).toBe(good);
  });
});
