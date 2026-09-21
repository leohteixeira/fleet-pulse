import { describe, expect, it } from 'vitest';

import {
  OFFLINE_MS,
  TIMEOUT_COPY,
  commandButtons,
  commandLine,
  deriveState,
  headerKpis,
  headingBucket,
  iconIdentity,
  initialState,
  planMarkerUpdate,
  reducer,
  vehicleCommands,
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

  it('keeps in-flight and queued commands per VIN and ignores a bad command payload', () => {
    const hydrated = reducer(initialState, { type: 'hydrate', snapshot: snapshot(2) });
    const sent = reducer(hydrated, {
      type: 'command',
      now,
      payload: {
        id: 'cmd-1',
        vin: 'FPULSESAO00000001',
        action: 'lock',
        state: 'SENT',
      },
    });
    const queued = reducer(sent, {
      type: 'command',
      now: now + 1,
      payload: {
        id: 'cmd-2',
        vin: 'FPULSESAO00000001',
        action: 'unlock',
        state: 'PENDING',
      },
    });
    const slot = vehicleCommands(queued.commands['FPULSESAO00000001']);
    expect(slot.inFlight?.id).toBe('cmd-1');
    expect(slot.queued?.id).toBe('cmd-2');
    expect(commandButtons(queued.commands['FPULSESAO00000001'])).toEqual({
      unlockDisabled: true,
      lockDisabled: true,
      unlockLabel: 'Destravar',
      lockLabel: 'Aguardando…',
    });

    const unchanged = reducer(queued, { type: 'command', now: now + 2, payload: '{not-json' });
    expect(unchanged).toBe(queued);
  });

  it('promotes the queued command after the in-flight command terminals', () => {
    let state = reducer(initialState, { type: 'hydrate', snapshot: snapshot(1) });
    state = reducer(state, {
      type: 'command',
      now,
      payload: { id: 'a', vin: 'FPULSESAO00000001', action: 'lock', state: 'SENT' },
    });
    state = reducer(state, {
      type: 'command',
      now: now + 1,
      payload: { id: 'b', vin: 'FPULSESAO00000001', action: 'unlock', state: 'PENDING' },
    });
    state = reducer(state, {
      type: 'command',
      now: now + 2,
      payload: { id: 'a', vin: 'FPULSESAO00000001', action: 'lock', state: 'ACKED' },
    });
    const slot = vehicleCommands(state.commands['FPULSESAO00000001']);
    expect(slot.inFlight?.id).toBe('b');
    expect(slot.inFlight?.state).toBe('PENDING');
    expect(slot.queued).toBeNull();
    expect(commandLine(slot.inFlight).label).toBe('PENDENTE');
    expect(commandButtons(state.commands['FPULSESAO00000001'])).toEqual({
      unlockDisabled: true,
      lockDisabled: false,
      unlockLabel: 'Enviando…',
      lockLabel: 'Travar',
    });
  });

  it('does not regress SENT to a late PENDING for the same id', () => {
    let state = reducer(initialState, { type: 'hydrate', snapshot: snapshot(1) });
    state = reducer(state, {
      type: 'command',
      now,
      payload: { id: 'late', vin: 'FPULSESAO00000001', action: 'unlock', state: 'SENT' },
    });
    const replay = reducer(state, {
      type: 'command',
      now: now + 1,
      payload: { id: 'late', vin: 'FPULSESAO00000001', action: 'unlock', state: 'PENDING' },
    });
    expect(replay.commands['FPULSESAO00000001']?.[0]?.state).toBe('SENT');
    expect(replay).toBe(state);
  });

  it('prefers SENT as in-flight when a PENDING successor is listed first', () => {
    const slot = vehicleCommands([
      { id: 'q', vin: 'v', action: 'unlock', state: 'PENDING' },
      { id: 's', vin: 'v', action: 'lock', state: 'SENT' },
    ]);
    expect(slot.inFlight?.id).toBe('s');
    expect(slot.queued?.id).toBe('q');
  });

  it('records a refused command as falhou', () => {
    let state = reducer(initialState, { type: 'hydrate', snapshot: snapshot(1) });
    state = reducer(state, {
      type: 'command',
      now,
      payload: { id: 'fail', vin: 'FPULSESAO00000001', action: 'lock', state: 'FAILED' },
    });
    expect(state.feed[0]?.kind).toBe('falhou');
    expect(state.feed[0]?.description).toBe('Veículo recusou o comando');
    expect(commandLine(vehicleCommands(state.commands['FPULSESAO00000001']).inFlight).label).toBe('FALHOU');
  });

  it('records area-exit and timeout copy with 5 segundos', () => {
    let state = reducer(initialState, { type: 'hydrate', snapshot: snapshot(1) });
    state = reducer(state, {
      type: 'area-exit',
      now,
      payload: { vin: 'FPULSESAO00000001', displayId: 'V01', lat: -23.55, lng: -46.686 },
    });
    expect(state.feed[0]?.kind).toBe('fora');
    expect(deriveState({ ...state.vehicles['FPULSESAO00000001']!, lng: -46.686, lastSeen: now }, state.polygon, now)).toBe(
      'fora',
    );

    state = reducer(state, {
      type: 'command',
      now: now + 1,
      payload: { id: 'off', vin: 'FPULSESAO00000001', action: 'unlock', state: 'TIMEOUT' },
    });
    expect(state.feed[0]?.description).toBe(TIMEOUT_COPY);
    expect(TIMEOUT_COPY).toContain('5 segundos');
    expect(commandLine(vehicleCommands(state.commands['FPULSESAO00000001']).inFlight).label).toBe('EXPIROU');
  });

  it('disables the same in-flight action and keeps the other queueable', () => {
    expect(
      commandButtons([{ id: '1', vin: 'v', action: 'unlock', state: 'SENT' }]),
    ).toEqual({
      unlockDisabled: true,
      lockDisabled: false,
      unlockLabel: 'Aguardando…',
      lockLabel: 'Travar',
    });
    expect(
      commandButtons([{ id: '2', vin: 'v', action: 'lock', state: 'PENDING' }]),
    ).toEqual({
      unlockDisabled: false,
      lockDisabled: true,
      unlockLabel: 'Destravar',
      lockLabel: 'Enviando…',
    });
  });

  it('counts header KPIs including Fora and Pendentes', () => {
    const hydrated = reducer(initialState, { type: 'hydrate', snapshot: snapshot(3) });
    const patched = reducer(hydrated, {
      type: 'patch',
      now,
      payload: {
        vin: 'FPULSESAO00000001',
        lat: -23.55,
        lng: -46.686,
        speed: 20,
        ignition: true,
        locked: false,
      },
    });
    const withCmd = reducer(patched, {
      type: 'command',
      now,
      payload: { id: 'p', vin: 'FPULSESAO00000001', action: 'lock', state: 'SENT' },
    });
    const kpis = headerKpis(Object.values(withCmd.vehicles), withCmd.polygon, now, withCmd.commands);
    expect(kpis.vehicles).toBe(3);
    expect(kpis.fora).toBe(1);
    expect(kpis.offline).toBe(2);
    expect(kpis.pendentes).toBe(1);
  });
});
