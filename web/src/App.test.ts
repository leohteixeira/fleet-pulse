import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  SNAPSHOT_ERROR,
  connectTelemetry,
  loadSnapshot,
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

    cleanup();
    expect(created?.closed).toBe(true);
  });
});
