import { StrictMode, useEffect, useReducer, useRef, useState } from 'react';

import { Feed } from './Feed';
import { MapView } from './MapView';
import { Panel } from './Panel';
import {
  headerKpis,
  initialState,
  reducer,
  vehicleCommands,
  type Action,
  type CommandAction,
  type Snapshot,
} from './state';
import { FREEZE_COPY, STREAM_DOWN, STREAM_LIVE, type StreamStatus } from './stream';
import { applyTheme, readTheme, type Theme } from './theme';

export const SNAPSHOT_ERROR = 'Não foi possível carregar a frota.';

export type StreamSource = {
  addEventListener(type: string, listener: (event: Event) => void): void;
  removeEventListener(type: string, listener: (event: Event) => void): void;
  close(): void;
};

export type { StreamStatus } from './stream';
export { FREEZE_COPY, STREAM_DOWN, STREAM_LIVE, commandsBlocked } from './stream';

export function App() {
  const [state, dispatch] = useReducer(reducer, initialState);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [doorFlash, setDoorFlash] = useState<'ok' | 'err' | null>(null);
  const [theme, setTheme] = useState<Theme>(() => readTheme());
  const [stream, setStream] = useState<StreamStatus>({ live: true, retries: 0 });
  const [lastDataAt, setLastDataAt] = useState(() => Date.now());
  const flashFor = useRef<string | null>(null);

  useEffect(() => {
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void loadSnapshot(controller.signal)
      .then((snapshot) => {
        dispatch({ type: 'hydrate', snapshot });
        setError(null);
        setNow(Date.now());
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setError(err instanceof Error ? err.message : SNAPSHOT_ERROR);
      });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    if (!state.polygon) {
      return;
    }
    return connectTelemetry(dispatch, EventSource, (status) => {
      setStream(status);
      if (status.live) {
        setLastDataAt(Date.now());
      }
    });
  }, [state.polygon]);

  const selected = state.selectedVin ? state.vehicles[state.selectedVin] : undefined;
  const selectedCommands = selected ? state.commands[selected.vin] : undefined;
  const { inFlight } = vehicleCommands(selectedCommands);

  useEffect(() => {
    if (!inFlight || (inFlight.state !== 'ACKED' && inFlight.state !== 'FAILED')) {
      return;
    }
    const key = `${inFlight.id}:${inFlight.state}`;
    if (flashFor.current === key) {
      return;
    }
    flashFor.current = key;
    setDoorFlash(inFlight.state === 'ACKED' ? 'ok' : 'err');
    const timer = window.setTimeout(() => setDoorFlash(null), 1600);
    return () => window.clearTimeout(timer);
  }, [inFlight]);

  const vehicles = Object.values(state.vehicles);
  const kpis = headerKpis(vehicles, state.polygon, now, state.commands);

  return (
    <div className="shell">
      <header className="header">
        <h1>FLEET PULSE</h1>
        <div className="kpis">
          <Kpi label="Veículos" value={kpis.vehicles} tone="idle" />
          <Kpi label="Disponíveis" value={kpis.disponiveis} tone="ok" />
          <Kpi label="Em uso" value={kpis.emUso} tone="use" />
          <Kpi label="Fora" value={kpis.fora} tone="warn" />
          <Kpi label="Offline" value={kpis.offline} tone="idle" />
          <Kpi label="Pendentes" value={kpis.pendentes} tone="accent" />
        </div>
        <div className="header-tools">
          <ThemeToggle theme={theme} onChange={setTheme} />
          <span className={`stream-pill${stream.live ? ' is-live' : ' is-down'}`}>
            <span className="stream-dot" />
            {stream.live ? STREAM_LIVE : STREAM_DOWN(stream.retries)}
            {stream.live ? <span className="stream-clock">{formatClock(now)}</span> : null}
          </span>
        </div>
      </header>
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : (
        <>
          <div className="body">
            {state.polygon && vehicles.length > 0 ? (
              <MapView
                polygon={state.polygon}
                vehicles={vehicles}
                selectedVin={state.selectedVin}
                now={now}
                theme={theme}
                frozen={!stream.live}
                freezeCopy={FREEZE_COPY(Math.max(0, Math.round((now - lastDataAt) / 1000)))}
                onSelect={(vin) => dispatch({ type: 'select', vin })}
              />
            ) : (
              <div className="map-slot" />
            )}
            <Panel
              vehicle={selected ?? null}
              polygon={state.polygon}
              commands={selectedCommands}
              now={now}
              doorFlash={doorFlash}
              streamLive={stream.live}
              onClose={() => dispatch({ type: 'select', vin: null })}
              onUnlock={() => void sendCommand(selected?.vin, 'unlock', dispatch)}
              onLock={() => void sendCommand(selected?.vin, 'lock', dispatch)}
            />
          </div>
          <Feed
            events={state.feed}
            open={state.feedOpen}
            now={now}
            onToggle={() => dispatch({ type: 'toggle-feed' })}
            onSelect={(vin) => dispatch({ type: 'select', vin })}
          />
        </>
      )}
    </div>
  );
}

function ThemeToggle({ theme, onChange }: { theme: Theme; onChange: (theme: Theme) => void }) {
  const light = theme === 'light';
  return (
    <div className="theme-toggle" role="group" aria-label="Tema">
      <span className={`theme-knob${light ? ' is-light' : ''}`} aria-hidden="true" />
      <button
        type="button"
        className={`theme-opt${light ? ' is-active' : ''}`}
        title="Tema claro"
        aria-pressed={light}
        onClick={() => onChange('light')}
      >
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" aria-hidden="true">
          <circle cx="7" cy="7" r="2.6" />
          <path d="M7 1v1.6M7 11.4V13M1 7h1.6M11.4 7H13M2.8 2.8l1.1 1.1M10.1 10.1l1.1 1.1M2.8 11.2l1.1-1.1M10.1 3.9l1.1-1.1" />
        </svg>
      </button>
      <button
        type="button"
        className={`theme-opt${!light ? ' is-active' : ''}`}
        title="Tema escuro"
        aria-pressed={!light}
        onClick={() => onChange('dark')}
      >
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" aria-hidden="true">
          <path d="M11.5 8.6A5 5 0 0 1 5.4 2.5a5 5 0 1 0 6.1 6.1z" />
        </svg>
      </button>
    </div>
  );
}

function Kpi({ label, value, tone }: { label: string; value: number; tone: string }) {
  return (
    <div className="kpi">
      <span className={`kpi-dot tone-${tone}`} />
      <span className="kpi-label">{label}</span>
      <span className="kpi-value">{value}</span>
    </div>
  );
}

export function Root() {
  return (
    <StrictMode>
      <App />
    </StrictMode>
  );
}

export function connectTelemetry(
  dispatch: (action: Action) => void,
  EventSourceCtor: new (url: string) => StreamSource,
  onStatus?: (status: StreamStatus) => void,
): () => void {
  const source = new EventSourceCtor('/api/stream');
  let retries = 0;
  const markLive = () => {
    retries = 0;
    onStatus?.({ live: true, retries: 0 });
  };
  const onTelemetry = (event: Event) => {
    dispatch({ type: 'patch', payload: (event as MessageEvent<string>).data });
    markLive();
  };
  const onCommand = (event: Event) => {
    dispatch({ type: 'command', payload: (event as MessageEvent<string>).data });
    markLive();
  };
  const onAreaExit = (event: Event) => {
    dispatch({ type: 'area-exit', payload: (event as MessageEvent<string>).data });
    markLive();
  };
  const onOpen = () => {
    markLive();
  };
  const onError = () => {
    retries = Math.min(retries + 1, 8);
    onStatus?.({ live: false, retries });
  };
  source.addEventListener('telemetry', onTelemetry);
  source.addEventListener('command', onCommand);
  source.addEventListener('area-exit', onAreaExit);
  source.addEventListener('open', onOpen);
  source.addEventListener('error', onError);
  return () => {
    source.removeEventListener('telemetry', onTelemetry);
    source.removeEventListener('command', onCommand);
    source.removeEventListener('area-exit', onAreaExit);
    source.removeEventListener('open', onOpen);
    source.removeEventListener('error', onError);
    source.close();
  };
}

export function formatClock(now: number): string {
  return new Date(now).toLocaleTimeString('pt-BR', { hour12: false });
}

const sending = new Set<string>();

export async function sendCommand(
  vin: string | undefined,
  action: CommandAction,
  dispatch: (action: Action) => void,
): Promise<void> {
  if (!vin || sending.has(vin)) {
    return;
  }
  sending.add(vin);
  try {
    let response: Response;
    try {
      response = await fetch(`/api/vehicles/${vin}/${action}`, {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
      });
    } catch {
      return;
    }
    if (response.status === 409 || !response.ok) {
      return;
    }
    let body: unknown;
    try {
      body = (await response.json()) as unknown;
    } catch {
      return;
    }
    if (body === null || typeof body !== 'object') {
      return;
    }
    const rec = body as { id?: unknown; state?: unknown };
    if (typeof rec.id !== 'string' || typeof rec.state !== 'string') {
      return;
    }
    dispatch({
      type: 'command',
      payload: { id: rec.id, vin, action, state: rec.state },
    });
  } finally {
    sending.delete(vin);
  }
}

export async function loadSnapshot(signal: AbortSignal): Promise<Snapshot> {
  let response: Response;
  try {
    response = await fetch('/api/vehicles', { signal });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    throw new Error(SNAPSHOT_ERROR);
  }
  if (!response.ok) {
    throw new Error(SNAPSHOT_ERROR);
  }
  let body: unknown;
  try {
    body = (await response.json()) as unknown;
  } catch {
    throw new Error(SNAPSHOT_ERROR);
  }
  if (!isSnapshot(body)) {
    throw new Error(SNAPSHOT_ERROR);
  }
  return body;
}

function isSnapshot(value: unknown): value is Snapshot {
  if (value === null || typeof value !== 'object') {
    return false;
  }
  const rec = value as Record<string, unknown>;
  if (!isPolygon(rec.polygon) || !Array.isArray(rec.vehicles)) {
    return false;
  }
  return rec.vehicles.every((item) => {
    return item !== null && typeof item === 'object' && typeof (item as { vin?: unknown }).vin === 'string';
  });
}

function isPolygon(value: unknown): value is Snapshot['polygon'] {
  if (value === null || typeof value !== 'object') {
    return false;
  }
  const rec = value as Record<string, unknown>;
  return (
    typeof rec.south === 'number' &&
    typeof rec.north === 'number' &&
    typeof rec.west === 'number' &&
    typeof rec.east === 'number'
  );
}
