import L from 'leaflet';
import { useEffect, useRef } from 'react';
import { MapContainer, Rectangle, TileLayer, useMap } from 'react-leaflet';

import { CAR_BODY, CAR_GLASS, escapeAttr, escapeHtml } from './carIcon';
import { shouldUpdateLatLng, tileURL } from './mapChrome';
import {
  PRESENTATION_LABELS,
  deriveState,
  iconIdentity,
  planMarkerUpdate,
  type Polygon,
  type PresentationState,
  type Vehicle,
} from './state';

type MapViewProps = {
  polygon: Polygon;
  vehicles: Vehicle[];
  selectedVin: string | null;
  now: number;
  theme: 'dark' | 'light';
  frozen: boolean;
  freezeCopy: string;
  onSelect: (vin: string | null) => void;
};

export function MapView({
  polygon,
  vehicles,
  selectedVin,
  now,
  theme,
  frozen,
  freezeCopy,
  onSelect,
}: MapViewProps) {
  const center: [number, number] = [
    (polygon.south + polygon.north) / 2,
    (polygon.west + polygon.east) / 2,
  ];

  return (
    <div className="map-wrap">
      {frozen ? <div className="map-band" aria-hidden="true" /> : null}
      {frozen ? (
        <div className="map-freeze" role="status">
          {freezeCopy}
        </div>
      ) : null}
    <MapContainer
      className="map"
      center={center}
      zoom={14}
      zoomControl
      attributionControl
    >
      <TileLayer
        key={theme}
        url={tileURL(theme)}
        attribution="© Esri, HERE, OpenStreetMap"
        maxZoom={16}
      />
      <Rectangle
        bounds={[
          [polygon.south, polygon.west],
          [polygon.north, polygon.east],
        ]}
        pathOptions={{
          color: 'var(--text-3)',
          weight: 1,
          dashArray: '6 6',
          fill: true,
          fillColor: 'var(--accent)',
          fillOpacity: 0.03,
          interactive: false,
        }}
      />
      <VehicleMarkers
        vehicles={vehicles}
        polygon={polygon}
        selectedVin={selectedVin}
        now={now}
        frozen={frozen}
        onSelect={onSelect}
      />
    </MapContainer>
    </div>
  );
}

type MarkerEntry = {
  marker: L.Marker;
  key: string;
};

function VehicleMarkers({
  vehicles,
  polygon,
  selectedVin,
  now,
  frozen,
  onSelect,
}: {
  vehicles: Vehicle[];
  polygon: Polygon;
  selectedVin: string | null;
  now: number;
  frozen: boolean;
  onSelect: (vin: string | null) => void;
}) {
  const map = useMap();
  const markers = useRef(new Map<string, MarkerEntry>());
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  useEffect(() => {
    const onMapClick = () => onSelectRef.current(null);
    map.on('click', onMapClick);
    return () => {
      map.off('click', onMapClick);
    };
  }, [map]);

  useEffect(() => {
    const keep = new Set<string>();
    for (const vehicle of vehicles) {
      keep.add(vehicle.vin);
      const selected = vehicle.vin === selectedVin;
      const presentation = deriveState(vehicle, polygon, now);
      const key = iconIdentity(presentation, selected, vehicle.heading);
      const existing = markers.current.get(vehicle.vin);
      const plan = planMarkerUpdate(existing?.key, key);
      if (plan === 'create' || !existing) {
        const marker = L.marker([vehicle.lat, vehicle.lng], {
          icon: makeIcon(vehicle, presentation, selected),
          zIndexOffset: selected ? 1000 : 0,
        }).addTo(map);
        marker.on('click', (event) => {
          L.DomEvent.stopPropagation(event);
          onSelectRef.current(vehicle.vin);
        });
        markers.current.set(vehicle.vin, { marker, key });
        continue;
      }
      if (shouldUpdateLatLng(frozen)) {
        existing.marker.setLatLng([vehicle.lat, vehicle.lng]);
      }
      if (plan === 'rebuild') {
        existing.marker.setIcon(makeIcon(vehicle, presentation, selected));
        existing.marker.setZIndexOffset(selected ? 1000 : 0);
        existing.key = key;
      }
    }
    for (const [vin, entry] of markers.current) {
      if (keep.has(vin)) {
        continue;
      }
      entry.marker.remove();
      markers.current.delete(vin);
    }
  }, [frozen, map, now, polygon, selectedVin, vehicles]);

  useEffect(() => {
    return () => {
      for (const entry of markers.current.values()) {
        entry.marker.remove();
      }
      markers.current.clear();
    };
  }, [map]);

  return null;
}

function makeIcon(vehicle: Vehicle, presentation: PresentationState, selected: boolean): L.DivIcon {
  const offline = presentation === 'offline';
  const fill = fillFor(presentation);
  const stroke = offline ? 'var(--text-3)' : 'rgba(0,0,0,.6)';
  const dash = offline ? '2 1.5' : '';
  const win = offline ? 'rgba(91,100,114,.5)' : 'var(--glass)';
  const chipColor = selected ? '#fff' : offline ? 'var(--text-2)' : fill;
  const chipBg = selected ? 'var(--accent)' : 'var(--chip)';
  const chipBorder = selected ? 'var(--accent)' : 'var(--chip-border)';
  const rot = vehicle.heading;
  const ring = selected ? '<span class="marker-ring"></span>' : '';
  const html = `<div class="marker" data-vin="${escapeAttr(vehicle.vin)}" data-state="${presentation}" data-label="${escapeAttr(PRESENTATION_LABELS[presentation])}" style="opacity:${offline ? 0.85 : 1}">${ring}<svg width="14" height="24" viewBox="0 0 12 22" style="display:block;transform:rotate(${rot}deg);transform-origin:50% 50%;filter:drop-shadow(0 1px 1px rgba(0,0,0,.8))"><path d="${CAR_BODY}" fill="${fill}" stroke="${stroke}" stroke-width="1"${dash ? ` stroke-dasharray="${dash}"` : ''}/><path d="${CAR_GLASS}" fill="${win}"/></svg><span class="marker-chip" style="color:${chipColor};background:${chipBg};border-color:${chipBorder}">${escapeHtml(vehicle.displayId)}</span></div>`;

  return L.divIcon({
    html,
    className: 'marker-icon',
    iconSize: [44, 44],
    iconAnchor: [22, 12],
  });
}

function fillFor(presentation: PresentationState): string {
  switch (presentation) {
    case 'disponivel':
      return 'var(--ok)';
    case 'em_uso':
      return 'var(--v-in-use)';
    case 'fora':
      return 'var(--warn)';
    case 'offline':
      return 'var(--v-offline)';
  }
}

