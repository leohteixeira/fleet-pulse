import { StrictMode, useEffect, useRef, useState } from 'react';
import { BrowserRouter, Route, Routes, useLocation } from 'react-router-dom';

import { App } from './App';
import { Carteira } from './Carteira';
import { loadClock } from './carteiraApi';
import type { ClockSnapshot } from './carteiraState';
import { fleetForPath } from './routes';
import { connectStream, type StreamStatus } from './stream';
import { StreamContext, type StreamListener } from './streamContext';
import { applyTheme, readTheme, type Theme } from './theme';

export function Root() {
  return (
    <StrictMode>
      <BrowserRouter>
        <Shell />
      </BrowserRouter>
    </StrictMode>
  );
}

export function Shell() {
  const location = useLocation();
  const fleet = fleetForPath(location.pathname);
  const [theme, setTheme] = useState<Theme>(() => readTheme());
  const [stream, setStream] = useState<StreamStatus>({ live: true, retries: 0 });
  const [lastDataAt, setLastDataAt] = useState(() => Date.now());
  const [now, setNow] = useState(() => Date.now());
  const [clock, setClock] = useState<ClockSnapshot | null>(null);
  const listeners = useRef(new Set<StreamListener>());

  useEffect(() => {
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    if (fleet !== 'leasing') {
      return;
    }
    const controller = new AbortController();
    const tick = () => {
      void loadClock(controller.signal)
        .then(setClock)
        .catch((err: unknown) => {
          if (err instanceof DOMException && err.name === 'AbortError') {
            return;
          }
        });
    };
    tick();
    const timer = setInterval(tick, 1000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [fleet]);

  useEffect(() => {
    return connectStream({
      fleet,
      EventSourceCtor: EventSource,
      onEvent: (type, data) => {
        for (const fn of listeners.current) {
          fn(type, data);
        }
        setLastDataAt(Date.now());
      },
      onStatus: (status) => {
        setStream(status);
        if (status.live) {
          setLastDataAt(Date.now());
        }
      },
    });
  }, [fleet]);

  return (
    <StreamContext.Provider
      value={{
        status: stream,
        lastDataAt,
        now,
        theme,
        setTheme,
        clock,
        subscribe: (fn) => {
          listeners.current.add(fn);
          return () => {
            listeners.current.delete(fn);
          };
        },
      }}
    >
      <Routes>
        <Route path="/" element={<App />} />
        <Route path="/carteira/:id?" element={<Carteira />} />
      </Routes>
    </StreamContext.Provider>
  );
}
