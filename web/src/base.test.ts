import { afterEach, expect, it, vi } from 'vitest';
import { loadSnapshot } from './App';
import { loadClock } from './carteiraApi';
import { streamURL } from './stream';

vi.mock('./MapView', () => ({ MapView: () => null }));
afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

it('keeps snapshot, portfolio and SSE requests under the deployment prefix', async () => {
  vi.stubEnv('BASE_URL', '/fleet-pulse/');
  const fetcher = vi.fn().mockResolvedValue({ ok: false });
  vi.stubGlobal('fetch', fetcher);
  await expect(loadSnapshot(new AbortController().signal)).rejects.toThrow();
  await expect(loadClock()).rejects.toThrow();
  expect(fetcher.mock.calls.map(([url]) => url)).toEqual([
    '/fleet-pulse/api/vehicles',
    '/fleet-pulse/api/clock',
  ]);
  expect(streamURL('leasing')).toBe('/fleet-pulse/api/stream?fleet=leasing');
});
