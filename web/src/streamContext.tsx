import { createContext, useContext } from 'react';

import type { ClockSnapshot } from './carteiraState';
import type { StreamEventType, StreamStatus } from './stream';
import type { Theme } from './theme';

export type StreamListener = (type: StreamEventType, data: string) => void;

export type StreamBus = {
  status: StreamStatus;
  lastDataAt: number;
  now: number;
  theme: Theme;
  setTheme: (theme: Theme) => void;
  clock: ClockSnapshot | null;
  subscribe: (fn: StreamListener) => () => void;
};

export const StreamContext = createContext<StreamBus | null>(null);

export function useStreamBus(): StreamBus {
  const ctx = useContext(StreamContext);
  if (!ctx) {
    throw new Error('StreamContext missing');
  }
  return ctx;
}
