export const STREAM_LIVE = 'STREAM · AO VIVO';
export const STREAM_DOWN = (n: number) => `STREAM CAIU · RECONECTANDO (${n}/8)`;
export const FREEZE_COPY = (seconds: number) => `Posições congeladas · último dado há ${seconds}s`;

export type StreamStatus = {
  live: boolean;
  retries: number;
};

export function commandsBlocked(streamLive: boolean): boolean {
  return !streamLive;
}
