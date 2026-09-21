import type { ReactNode } from 'react';
import { NavLink } from 'react-router-dom';

import { COPY } from './carteiraCopy';
import { formatRealTooltip, formatSimChip, type ClockSnapshot } from './carteiraState';
import { STREAM_DOWN, STREAM_LIVE, type StreamStatus } from './stream';
import type { Theme } from './theme';

type HeaderProps = {
  variant: 'frota' | 'carteira';
  theme: Theme;
  onTheme: (theme: Theme) => void;
  stream: StreamStatus;
  now: number;
  clock?: ClockSnapshot | null;
  children?: ReactNode;
};

export function ConsoleHeader({
  variant,
  theme,
  onTheme,
  stream,
  now,
  clock,
  children,
}: HeaderProps) {
  return (
    <header className="header console-header">
      <div className="wordmark">
        <span className="wordmark-square" aria-hidden="true" />
        <span className="wordmark-title">FLEET PULSE</span>
        <span className="wordmark-sub">{variant === 'carteira' ? COPY.wordmark : 'SP · zona centro'}</span>
      </div>
      <nav className="console-tabs" aria-label="Consoles">
        <NavLink to="/" end className={({ isActive }) => `console-tab${isActive ? ' is-active' : ''}`}>
          {COPY.tabFrota}
        </NavLink>
        <NavLink
          to="/carteira"
          className={({ isActive }) => `console-tab${isActive ? ' is-active' : ''}`}
        >
          {COPY.tabCarteira}
        </NavLink>
      </nav>
      {children}
      <div className="header-tools">
        {variant === 'carteira' ? <SimClock clock={clock} /> : null}
        <ThemeToggle theme={theme} onChange={onTheme} />
        <span className={`stream-pill${stream.live ? ' is-live' : ' is-down'}`}>
          <span className="stream-dot" />
          {stream.live ? STREAM_LIVE : STREAM_DOWN(stream.retries)}
          {stream.live ? <span className="stream-clock">{formatClock(now)}</span> : null}
        </span>
      </div>
    </header>
  );
}

function SimClock({ clock }: { clock?: ClockSnapshot | null }) {
  const multiplier = clock?.multiplier ?? 14400;
  const sim = clock ? formatSimChip(clock.simulated) : '—';
  const title = clock ? formatRealTooltip(clock.real) : undefined;
  return (
    <div className="sim-clock" title={title}>
      <span className="sim-clock-label">
        <span className="sim-clock-dot" aria-hidden="true" />
        {COPY.clockPrefix} · ×{multiplier}
      </span>
      <span className="sim-clock-value">{sim}</span>
    </div>
  );
}

export function ThemeToggle({ theme, onChange }: { theme: Theme; onChange: (theme: Theme) => void }) {
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

export function formatClock(now: number): string {
  return new Date(now).toLocaleTimeString('pt-BR', { hour12: false });
}
