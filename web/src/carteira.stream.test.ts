import { afterEach, describe, expect, it } from 'vitest';

import {
  STREAM_DOWN,
  connectStream,
  createStreamController,
  resetSharedStream,
  streamURL,
  type StreamSource,
  type StreamStatus,
} from './stream';

class FakeEventSource implements StreamSource {
  readonly url: string;
  closed = false;
  private readonly listeners = new Map<string, Set<(event: Event) => void>>();

  constructor(url: string) {
    this.url = url;
  }

  addEventListener(type: string, listener: (event: Event) => void): void {
    const set = this.listeners.get(type) ?? new Set();
    set.add(listener);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, listener: (event: Event) => void): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closed = true;
    this.emit('error', '');
  }

  emit(type: string, data: string): void {
    const event = { data } as MessageEvent<string>;
    for (const listener of this.listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

describe('controlled fleet swap', () => {
  afterEach(() => {
    resetSharedStream();
  });

  it('does not call STREAM_DOWN or increment retries', () => {
    const statuses: StreamStatus[] = [];
    const sources: FakeEventSource[] = [];
    const handle = createStreamController({
      fleet: 'rental',
      EventSourceCtor: class extends FakeEventSource {
        constructor(url: string) {
          super(url);
          sources.push(this);
        }
      },
      onEvent: () => undefined,
      onStatus: (status) => {
        statuses.push(status);
      },
    });

    expect(handle.url).toBe(streamURL('rental'));
    handle.swapFleet('leasing');
    expect(sources[0]?.closed).toBe(true);
    expect(sources[1]?.url).toBe('/api/stream?fleet=leasing');
    expect(statuses.filter((status) => !status.live)).toHaveLength(0);
    expect(statuses.some((status) => STREAM_DOWN(status.retries) && !status.live)).toBe(false);
    expect(statuses.at(-1)).toEqual({ live: true, retries: 0 });

    sources[1]?.emit('error', '');
    expect(statuses.at(-1)).toEqual({ live: false, retries: 1 });
    handle.close();
  });

  it('guards StrictMode double mount without a second EventSource', () => {
    const sources: FakeEventSource[] = [];
    const Ctor = class extends FakeEventSource {
      constructor(url: string) {
        super(url);
        sources.push(this);
      }
    };
    const first = connectStream({
      fleet: 'rental',
      EventSourceCtor: Ctor,
      onEvent: () => undefined,
    });
    const second = connectStream({
      fleet: 'leasing',
      EventSourceCtor: Ctor,
      onEvent: () => undefined,
    });
    expect(sources).toHaveLength(2);
    expect(sources[1]?.url).toBe('/api/stream?fleet=leasing');
    first();
    second();
  });
});
