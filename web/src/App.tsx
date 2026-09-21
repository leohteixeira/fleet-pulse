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

export const SNAPSHOT_ERROR = 'Não foi possível carregar a frota.';

export type StreamSource = {
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  removeEventListener(type: string, listener: (event: MessageEvent<string>) => void): void;
  close(): void;
};

export function App() {
  const [state, dispatch] = useReducer(reducer, initialState);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [doorFlash, setDoorFlash] = useState<'ok' | 'err' | null>(null);
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
    if (!state.polygon) {
      return;
    }
    return connectTelemetry(dispatch, EventSource);
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
): () => void {
  const source = new EventSourceCtor('/api/stream');
  const onTelemetry = (event: MessageEvent<string>) => {
    dispatch({ type: 'patch', payload: event.data });
  };
  const onCommand = (event: MessageEvent<string>) => {
    dispatch({ type: 'command', payload: event.data });
  };
  const onAreaExit = (event: MessageEvent<string>) => {
    dispatch({ type: 'area-exit', payload: event.data });
  };
  source.addEventListener('telemetry', onTelemetry);
  source.addEventListener('command', onCommand);
  source.addEventListener('area-exit', onAreaExit);
  return () => {
    source.removeEventListener('telemetry', onTelemetry);
    source.removeEventListener('command', onCommand);
    source.removeEventListener('area-exit', onAreaExit);
    source.close();
  };
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
