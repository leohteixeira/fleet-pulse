import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  SNAPSHOT_ERROR,
  connectTelemetry,
  loadSnapshot,
  sendCommand,
  type StreamSource,
} from './App';
import { initialState, reducer, type Action, type FleetState, type Snapshot } from './state';

vi.mock('./MapView', () => ({ MapView: () => null }));

const centro = {
  south: -23.585,
  north: -23.525,
  west: -46.685,
  east: -46.6,
};

function snapshot(count = 20): Snapshot {
  return {
    polygon: centro,
    vehicles: Array.from({ length: count }, (_, i) => ({
      vin: `FPULSESAO${String(i + 1).padStart(8, '0')}`,
      displayId: `V${String(i + 1).padStart(2, '0')}`,
      lat: -23.55,
      lng: -46.63,
    })),
  };
}

class FakeEventSource implements StreamSource {
  readonly url: string;
  closed = false;
  private readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>();

  constructor(url: string) {
    this.url = url;
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: (event: MessageEvent<string>) => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
  }

  emit(type: string, data: string): void {
    const event = { data } as MessageEvent<string>;
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

describe('loadSnapshot', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns the Portuguese error on HTTP or JSON failure', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('nope', { status: 500 })));
    await expect(loadSnapshot(new AbortController().signal)).rejects.toThrow(SNAPSHOT_ERROR);

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{not-json', { status: 200, headers: { 'Content-Type': 'application/json' } })),
    );
    await expect(loadSnapshot(new AbortController().signal)).rejects.toThrow(SNAPSHOT_ERROR);
  });

  it('hydrates 20 vehicles with the silent VIN lastSeen null', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(snapshot(20)), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    const loaded = await loadSnapshot(new AbortController().signal);
    const next = reducer(initialState, { type: 'hydrate', snapshot: loaded });
    expect(Object.keys(next.vehicles)).toHaveLength(20);
    expect(next.vehicles['FPULSESAO00000020']?.lastSeen).toBeNull();
  });
});

describe('connectTelemetry', () => {
  it('patches on telemetry, ignores default message, and closes on cleanup', () => {
    let state: FleetState = reducer(initialState, { type: 'hydrate', snapshot: snapshot(20) });
    const dispatch = (action: Action) => {
      state = reducer(state, action);
    };
    let created: FakeEventSource | undefined;
    const cleanup = connectTelemetry(dispatch, class extends FakeEventSource {
      constructor(url: string) {
        super(url);
        created = this;
      }
    });
    expect(created?.url).toBe('/api/stream');

    const before = state;
    created?.emit('message', JSON.stringify({ vin: 'FPULSESAO00000001', lat: -23.5, lng: -46.6 }));
    expect(state).toBe(before);

    created?.emit(
      'telemetry',
      JSON.stringify({ vin: 'FPULSESAO00000001', lat: -23.54, lng: -46.62 }),
    );
    expect(state.vehicles['FPULSESAO00000001']?.lat).toBe(-23.54);
    expect(state.vehicles['FPULSESAO00000001']?.lng).toBe(-46.62);
    expect(state.vehicles['FPULSESAO00000001']?.lastSeen).not.toBeNull();

    created?.emit(
      'command',
      JSON.stringify({
        id: 'cmd-1',
        vin: 'FPULSESAO00000001',
        action: 'unlock',
        state: 'SENT',
      }),
    );
    expect(state.commands['FPULSESAO00000001']?.[0]?.state).toBe('SENT');

    created?.emit(
      'area-exit',
      JSON.stringify({ vin: 'FPULSESAO00000001', displayId: 'V01', lat: -23.55, lng: -46.686 }),
    );
    expect(state.feed[0]?.kind).toBe('fora');

    cleanup();
    expect(created?.closed).toBe(true);
  });
});

describe('sendCommand', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('POSTs unlock and lock with Idempotency-Key, dispatches 202, and skips 409', async () => {
    vi.stubGlobal('crypto', { randomUUID: () => 'key-test' });
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      const id = url.endsWith('/lock') ? 'cmd-lock' : 'cmd-unlock';
      return new Response(JSON.stringify({ id, state: 'SENT' }), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      });
    });
    vi.stubGlobal('fetch', fetchMock);

    const dispatched: Action[] = [];
    const dispatch = (action: Action) => {
      dispatched.push(action);
    };

    await sendCommand('FPULSESAO00000001', 'unlock', dispatch);
    expect(fetchMock).toHaveBeenCalledWith('/api/vehicles/FPULSESAO00000001/unlock', {
      method: 'POST',
      headers: { 'Idempotency-Key': 'key-test' },
    });
    expect(dispatched).toEqual([
      {
        type: 'command',
        payload: { id: 'cmd-unlock', vin: 'FPULSESAO00000001', action: 'unlock', state: 'SENT' },
      },
    ]);

    await sendCommand('FPULSESAO00000001', 'lock', dispatch);
    expect(fetchMock).toHaveBeenCalledWith('/api/vehicles/FPULSESAO00000001/lock', {
      method: 'POST',
      headers: { 'Idempotency-Key': 'key-test' },
    });
    expect(dispatched).toHaveLength(2);
    expect(dispatched[1]).toEqual({
      type: 'command',
      payload: { id: 'cmd-lock', vin: 'FPULSESAO00000001', action: 'lock', state: 'SENT' },
    });

    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ id: 'busy', state: 'SENT' }), { status: 409 })),
    );
    await sendCommand('FPULSESAO00000001', 'unlock', dispatch);
    expect(dispatched).toHaveLength(2);
  });
});
