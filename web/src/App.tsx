import { StrictMode, useEffect, useReducer, useState } from 'react';

import { MapView } from './MapView';
import { initialState, reducer, type Action, type Snapshot } from './state';

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

  const vehicles = Object.values(state.vehicles);

  return (
    <div className="shell">
      <header className="header">
        <h1>FLEET PULSE</h1>
      </header>
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : state.polygon && vehicles.length > 0 ? (
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
  source.addEventListener('telemetry', onTelemetry);
  return () => {
    source.removeEventListener('telemetry', onTelemetry);
    source.close();
  };
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
