import {
  PRESENTATION_LABELS,
  commandButtons,
  commandLine,
  deriveState,
  vehicleCommands,
  type Command,
  type Polygon,
  type Vehicle,
} from './state';

const HEADINGS = ['N', 'NE', 'L', 'SE', 'S', 'SO', 'O', 'NO'] as const;

type PanelProps = {
  vehicle: Vehicle | null;
  polygon: Polygon | null;
  commands: Command[] | undefined;
  now: number;
  doorFlash: 'ok' | 'err' | null;
  onClose: () => void;
  onUnlock: () => void;
  onLock: () => void;
};

export function Panel({
  vehicle,
  polygon,
  commands,
  now,
  doorFlash,
  onClose,
  onUnlock,
  onLock,
}: PanelProps) {
  if (!vehicle) {
    return (
      <aside className="panel" aria-label="Painel do veículo">
        <div className="panel-empty">
          <div className="crosshair" aria-hidden="true">
            <span />
            <span />
          </div>
          <p>Nenhum veículo selecionado</p>
          <p className="panel-empty-hint">
            Clique em um marcador no mapa para ver telemetria e enviar comandos.
          </p>
        </div>
      </aside>
    );
  }

  const presentation = deriveState(vehicle, polygon, now);
  const { inFlight } = vehicleCommands(commands);
  const buttons = commandButtons(commands);
  const line = commandLine(inFlight);
  const ageMs = vehicle.lastSeen == null ? null : now - vehicle.lastSeen;
  const battTone = vehicle.battery < 20 ? 'err' : vehicle.battery < 40 ? 'warn' : 'ok';
  const seenTone = ageMs == null || ageMs > 60_000 ? 'err' : ageMs > 15_000 ? 'warn' : 'ok';
  const offline = presentation === 'offline';

  return (
    <aside className="panel" aria-label="Painel do veículo">
      <div className="panel-head">
        <div className="panel-head-main">
          <div className="panel-plate-row">
            <span className="panel-plate">{vehicle.plate}</span>
            <span className="panel-id">#{vehicle.displayId}</span>
          </div>
          <div className="panel-model">{vehicle.model}</div>
        </div>
        <span className={`state-chip state-${presentation}`}>
          <span className="state-dot" />
          {PRESENTATION_LABELS[presentation]}
        </span>
        <button type="button" className="panel-close" onClick={onClose} title="Fechar">
          ×
        </button>
      </div>

      <div className="panel-metrics">
        <div className="metric">
          <div className="metric-label">Bateria</div>
          <div className="metric-value">
            <span className={`tone-${battTone}`}>{vehicle.battery}</span>
            <span className="metric-unit">%</span>
          </div>
          <div className="batt-track">
            <div className={`batt-fill tone-${battTone}`} style={{ width: `${clamp(vehicle.battery, 0, 100)}%` }} />
          </div>
        </div>
        <div className="metric">
          <div className="metric-label">Velocidade</div>
          <div className="metric-value">
            <span>{offline ? '—' : vehicle.speed}</span>
            {!offline ? <span className="metric-unit">km/h</span> : null}
          </div>
          <div className="metric-sub">{headingCopy(vehicle.heading, vehicle.speed, offline)}</div>
        </div>
      </div>

      <dl className="panel-rows">
        <div className="panel-row">
          <dt>Ignição</dt>
          <dd>
            <span className={`row-dot ${vehicle.ignition ? 'tone-warn' : 'tone-idle'}`} />
            {vehicle.ignition ? 'LIGADA' : 'DESLIGADA'}
          </dd>
        </div>
        <div className={`panel-row ${doorFlash === 'ok' ? 'fp-flash-ok' : doorFlash === 'err' ? 'fp-flash-err' : ''}`}>
          <dt>Portas</dt>
          <dd>
            <span className={`row-dot ${vehicle.locked ? 'tone-ok' : 'tone-warn'}`} />
            {vehicle.locked ? 'TRAVADAS' : 'DESTRAVADAS'}
          </dd>
        </div>
        <div className="panel-row">
          <dt>Quilometragem</dt>
          <dd>
            <span>{formatKm(vehicle.odometer)}</span>
            <span className="row-muted">{tripCopy(vehicle.trip)}</span>
          </dd>
        </div>
        <div className="panel-row">
          <dt>Último sinal</dt>
          <dd className={`tone-${seenTone}`}>{lastSeenCopy(ageMs)}</dd>
        </div>
        <div className="panel-row">
          <dt>Posição</dt>
          <dd className="tone-muted">{formatCoord(vehicle.lat, vehicle.lng)}</dd>
        </div>
      </dl>

      <div className="panel-cmds">
        <div className="cmd-buttons">
          <button
            type="button"
            className={`btn-primary${inFlight?.state === 'PENDING' && inFlight.action === 'unlock' ? ' is-pending' : ''}`}
            disabled={buttons.unlockDisabled}
            onClick={onUnlock}
          >
            {inFlight?.state === 'PENDING' && inFlight.action === 'unlock' ? <span className="fp-spin" /> : null}
            {buttons.unlockLabel}
          </button>
          <button
            type="button"
            className={`btn-secondary${inFlight?.state === 'PENDING' && inFlight.action === 'lock' ? ' is-pending' : ''}`}
            disabled={buttons.lockDisabled}
            onClick={onLock}
          >
            {inFlight?.state === 'PENDING' && inFlight.action === 'lock' ? <span className="fp-spin" /> : null}
            {buttons.lockLabel}
          </button>
        </div>
        <div className={`cmd-line tone-${line.tone}`}>
          <span>COMANDO</span>
          <span>
            <span className={`cmd-dot${line.tone === 'accent' ? ' is-live' : ''}`} />
            {line.label}
          </span>
        </div>
      </div>
    </aside>
  );
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function headingCopy(heading: number, speed: number, offline: boolean): string {
  if (offline || speed <= 0) {
    return 'rumo parado';
  }
  const card = HEADINGS[Math.round(heading / 45) % 8] ?? 'N';
  return `rumo ${card} ${Math.round(heading)}°`;
}

function formatKm(value: number): string {
  return `${Math.round(value).toLocaleString('pt-BR')} km`;
}

function tripCopy(trip: number): string {
  if (trip <= 0) {
    return 'sem viagem ativa';
  }
  return `${trip.toFixed(1).replace('.', ',')} km nesta viagem`;
}

function lastSeenCopy(ageMs: number | null): string {
  if (ageMs == null) {
    return 'sem sinal';
  }
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

function formatCoord(lat: number, lng: number): string {
  return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}
