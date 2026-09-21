import { describe, expect, it } from 'vitest';

import { THEME_KEY, applyTheme, readTheme } from './theme';

function memoryStore(initial: Record<string, string> = {}) {
  const data = { ...initial };
  return {
    getItem(key: string): string | null {
      return data[key] ?? null;
    },
    setItem(key: string, value: string): void {
      data[key] = value;
    },
    data,
  };
}

describe('theme', () => {
  it('defaults to dark when storage is missing or invalid', () => {
    expect(readTheme(null)).toBe('dark');
    expect(readTheme(memoryStore({ [THEME_KEY]: 'purple' }))).toBe('dark');
  });

  it('persists light on the document and in storage', () => {
    const attrs = new Map<string, string>();
    const root = {
      setAttribute(name: string, value: string) {
        attrs.set(name, value);
      },
      removeAttribute(name: string) {
        attrs.delete(name);
      },
    };
    const store = memoryStore();
    applyTheme('light', root, store);
    expect(attrs.get('data-theme')).toBe('light');
    expect(store.getItem(THEME_KEY)).toBe('light');
    expect(readTheme(store)).toBe('light');

    applyTheme('dark', root, store);
    expect(attrs.has('data-theme')).toBe(false);
    expect(store.getItem(THEME_KEY)).toBe('dark');
  });
});
