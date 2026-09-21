import type { FeedEvent } from './state';

type FeedProps = {
  events: FeedEvent[];
  open: boolean;
  now: number;
  onToggle: () => void;
  onSelect: (vin: string) => void;
};

export function Feed({ events, open, now, onToggle, onSelect }: FeedProps) {
  const fails = events.filter((event) => event.kind === 'falhou').length;
  const exits = events.filter((event) => event.kind === 'fora').length;

  return (
    <footer className={`feed${open ? '' : ' is-collapsed'}`}>
      <button type="button" className="feed-head" onClick={onToggle}>
        <span className="feed-chevron">{open ? '▾' : '▸'}</span>
        <span>Eventos</span>
        <span className="feed-count">{events.length} registros</span>
        <span className="feed-stats">
          <span>
            <span className="tone-err">{fails}</span> falhas
          </span>
          <span>
            <span className="tone-warn">{exits}</span> fora da área
          </span>
        </span>
      </button>
      {open ? (
        <div className="feed-list">
          {events.map((event) => (
            <div key={event.id} className="feed-row">
              <span className="feed-when">{relativeTime(now - event.at)}</span>
              <button type="button" className="feed-vin" onClick={() => onSelect(event.vin)}>
                {event.displayId}
              </button>
              <span className={`feed-kind kind-${event.kind}`}>
                <span className="feed-kind-dot" />
                {kindLabel(event.kind)}
              </span>
              <span className="feed-desc">{event.description}</span>
            </div>
          ))}
        </div>
      ) : null}
    </footer>
  );
}

function kindLabel(kind: FeedEvent['kind']): string {
  switch (kind) {
    case 'enviado':
      return 'Enviado';
    case 'confirmado':
      return 'Confirmado';
    case 'falhou':
      return 'Falhou';
    case 'expirou':
      return 'Expirou';
    case 'fora':
      return 'Fora';
  }
}

function relativeTime(ageMs: number): string {
  const seconds = Math.max(0, Math.round(ageMs / 1000));
  if (seconds < 60) {
    return `há ${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `há ${minutes}min`;
  }
  return `há ${Math.floor(minutes / 60)}h`;
}
