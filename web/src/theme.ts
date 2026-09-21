export const THEME_KEY = 'fleetpulse-theme';

export type Theme = 'dark' | 'light';

export type ThemeStorage = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
};

export function readTheme(storage?: ThemeStorage | null): Theme {
  try {
    const store = storage ?? defaultStorage();
    if (!store) {
      return 'dark';
    }
    return store.getItem(THEME_KEY) === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

export function applyTheme(
  theme: Theme,
  root?: { setAttribute(name: string, value: string): void; removeAttribute(name: string): void },
  storage?: ThemeStorage | null,
): Theme {
  const target = root ?? (typeof document !== 'undefined' ? document.documentElement : undefined);
  if (target) {
    if (theme === 'light') {
      target.setAttribute('data-theme', 'light');
    } else {
      target.removeAttribute('data-theme');
    }
  }
  try {
    const store = storage ?? defaultStorage();
    store?.setItem(THEME_KEY, theme);
  } catch {
    // localStorage can be unavailable; the in-memory theme still applies.
  }
  return theme;
}

function defaultStorage(): ThemeStorage | null {
  try {
    return globalThis.localStorage;
  } catch {
    return null;
  }
}
