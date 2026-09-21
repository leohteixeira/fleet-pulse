export const STREAM_LIVE = 'STREAM · AO VIVO';
export const STREAM_DOWN = (n: number) => `STREAM CAIU · RECONECTANDO (${n}/8)`;
export const FREEZE_COPY = (seconds: number) => `Posições congeladas · último dado há ${seconds}s`;

export type Fleet = 'rental' | 'leasing';

export type StreamStatus = {
  live: boolean;
  retries: number;
};

export type StreamEventType = 'telemetry' | 'command' | 'area-exit';

export type StreamSource = {
  addEventListener(type: string, listener: (event: Event) => void): void;
  removeEventListener(type: string, listener: (event: Event) => void): void;
  close(): void;
};

export type StreamHandlers = {
  onEvent: (type: StreamEventType, data: string) => void;
  onStatus?: (status: StreamStatus) => void;
};

export type StreamControllerOptions = StreamHandlers & {
  fleet: Fleet;
  EventSourceCtor: new (url: string) => StreamSource;
};

export type StreamController = {
  url: string;
  fleet: Fleet;
  close: () => void;
  swapFleet: (fleet: Fleet) => void;
  setHandlers: (handlers: StreamHandlers) => void;
};

export function streamURL(fleet: Fleet): string {
  return `/api/stream?fleet=${fleet}`;
}

export function commandsBlocked(streamLive: boolean): boolean {
  return !streamLive;
}

export function createStreamController(opts: StreamControllerOptions): StreamController {
  let fleet = opts.fleet;
  let retries = 0;
  let handlers: StreamHandlers = { onEvent: opts.onEvent, onStatus: opts.onStatus };
  let closed = false;
  let source: StreamSource | null = null;
  const listeners = new Map<string, (event: Event) => void>();

  const markLive = () => {
    retries = 0;
    handlers.onStatus?.({ live: true, retries: 0 });
  };

  const attach = (next: StreamSource) => {
    const onTelemetry = (event: Event) => {
      handlers.onEvent('telemetry', (event as MessageEvent<string>).data);
      markLive();
    };
    const onCommand = (event: Event) => {
      handlers.onEvent('command', (event as MessageEvent<string>).data);
      markLive();
    };
    const onAreaExit = (event: Event) => {
      handlers.onEvent('area-exit', (event as MessageEvent<string>).data);
      markLive();
    };
    const onOpen = () => {
      markLive();
    };
    const onError = () => {
      retries = Math.min(retries + 1, 8);
      handlers.onStatus?.({ live: false, retries });
    };
    listeners.set('telemetry', onTelemetry);
    listeners.set('command', onCommand);
    listeners.set('area-exit', onAreaExit);
    listeners.set('open', onOpen);
    listeners.set('error', onError);
    next.addEventListener('telemetry', onTelemetry);
    next.addEventListener('command', onCommand);
    next.addEventListener('area-exit', onAreaExit);
    next.addEventListener('open', onOpen);
    next.addEventListener('error', onError);
  };

  const detach = (current: StreamSource) => {
    for (const [type, listener] of listeners) {
      current.removeEventListener(type, listener);
    }
    listeners.clear();
  };

  const open = (nextFleet: Fleet): StreamSource => {
    const next = new opts.EventSourceCtor(streamURL(nextFleet));
    attach(next);
    return next;
  };

  source = open(fleet);

  const controller: StreamController = {
    get url() {
      return streamURL(fleet);
    },
    get fleet() {
      return fleet;
    },
    setHandlers(next) {
      handlers = next;
    },
    swapFleet(nextFleet) {
      if (closed || nextFleet === fleet || !source) {
        return;
      }
      detach(source);
      source.close();
      fleet = nextFleet;
      source = open(fleet);
      handlers.onStatus?.({ live: true, retries });
    },
    close() {
      if (closed) {
        return;
      }
      closed = true;
      if (source) {
        detach(source);
        source.close();
        source = null;
      }
    },
  };

  return controller;
}

type SharedStream = {
  controller: StreamController;
  refs: number;
};

let shared: SharedStream | null = null;

export function connectStream(opts: StreamControllerOptions): () => void {
  if (!shared) {
    shared = { controller: createStreamController(opts), refs: 0 };
  } else {
    shared.controller.setHandlers({ onEvent: opts.onEvent, onStatus: opts.onStatus });
    shared.controller.swapFleet(opts.fleet);
  }
  shared.refs += 1;
  const session = shared;
  return () => {
    session.refs -= 1;
    queueMicrotask(() => {
      if (session.refs === 0 && shared === session) {
        session.controller.close();
        shared = null;
      }
    });
  };
}

export function resetSharedStream(): void {
  shared?.controller.close();
  shared = null;
}
