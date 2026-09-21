import { describe, expect, it } from 'vitest';

import { shouldUpdateLatLng, tileURL } from './mapChrome';

describe('map chrome', () => {
  it('uses World Light Gray tiles in the light theme', () => {
    expect(tileURL('dark')).toContain('World_Dark_Gray_Base');
    expect(tileURL('light')).toContain('World_Light_Gray_Base');
  });

  it('freezes marker motion while the stream is down', () => {
    expect(shouldUpdateLatLng(false)).toBe(true);
    expect(shouldUpdateLatLng(true)).toBe(false);
  });
});
