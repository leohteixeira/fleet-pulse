import L from 'leaflet';
import { useEffect, useRef } from 'react';
import { MapContainer, TileLayer, useMap } from 'react-leaflet';

import { COPY } from './carteiraCopy';
import { leasingMarkerHtml } from './carteiraMarker';
import { GREATER_SP, leasingIconIdentity, type BookRow } from './carteiraState';
import { shouldUpdateLatLng, tileURL } from './mapChrome';
import { planMarkerUpdate } from './state';

type CarteiraMapProps = {
  rows: BookRow[];
  selectedId: string | null;
  theme: 'dark' | 'light';
  frozen: boolean;
  freezeCopy: string;
  panId: string | null;
  onPanDone?: () => void;
  onSelect: (id: string | null) => void;
};

export function CarteiraMap({
  rows,
  selectedId,
  theme,
  frozen,
  freezeCopy,
  panId,
  onPanDone,
  onSelect,
}: CarteiraMapProps) {
  const center: [number, number] = [
    (GREATER_SP.south + GREATER_SP.north) / 2,
    (GREATER_SP.west + GREATER_SP.east) / 2,
  ];

  return (
    <div className="map-wrap">
      {frozen ? <div className="map-band" aria-hidden="true" /> : null}
      {frozen ? (
        <div className="map-freeze" role="status">
          {freezeCopy}
        </div>
      ) : null}
      <MapContainer className="map" center={center} zoom={12} zoomControl attributionControl>
        <TileLayer
          key={theme}
          url={tileURL(theme)}
          attribution="© Esri, HERE, OpenStreetMap"
          maxZoom={16}
        />
        <FitBounds />
        <LeaseMarkers
          rows={rows}
          selectedId={selectedId}
          frozen={frozen}
          panId={panId}
          onPanDone={onPanDone}
          onSelect={onSelect}
        />
      </MapContainer>
      <div className="map-legend">
        <span>
          <i className="lg-swatch lg-ativo" />
          {COPY.legendAtivo}
        </span>
        <span>
          <i className="lg-swatch lg-armed" />
          {COPY.legendArmed}
        </span>
        <span>
          <i className="lg-swatch lg-blocked" />
          {COPY.legendBlocked}
        </span>
        <span>
          <i className="lg-swatch lg-unlock" />
          {COPY.legendUnlock}
        </span>
        <span>
          <i className="lg-swatch lg-off" />
          {COPY.legendOffline}
        </span>
        <span className="lg-hint">{COPY.legendChip}</span>
      </div>
    </div>
  );
}

function FitBounds() {
  const map = useMap();
  useEffect(() => {
    map.fitBounds(
      [
        [GREATER_SP.south, GREATER_SP.west],
        [GREATER_SP.north, GREATER_SP.east],
      ],
      { padding: [28, 36] },
    );
  }, [map]);
  return null;
}

type MarkerEntry = {
  marker: L.Marker;
  key: string;
};

function LeaseMarkers({
  rows,
  selectedId,
  frozen,
  panId,
  onPanDone,
  onSelect,
}: {
  rows: BookRow[];
  selectedId: string | null;
  frozen: boolean;
  panId: string | null;
  onPanDone?: () => void;
  onSelect: (id: string | null) => void;
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
    if (!panId) {
      return;
    }
    const row = rows.find((item) => item.id === panId);
    if (!row?.vehicle) {
      return;
    }
    const latlng = L.latLng(row.vehicle.lat, row.vehicle.lng);
    if (!map.getBounds().pad(-0.1).contains(latlng)) {
      map.panTo(latlng);
    }
    onPanDone?.();
  }, [map, onPanDone, panId]);

  useEffect(() => {
    const keep = new Set<string>();
    for (const row of rows) {
      if (!row.vehicle) {
        continue;
      }
      keep.add(row.id);
      const selected = row.id === selectedId;
      const key = leasingIconIdentity(
        row.vehicleState,
        selected,
        row.online,
        row.daysLate,
        row.vehicle.heading,
      );
      const existing = markers.current.get(row.id);
      const plan = planMarkerUpdate(existing?.key, key);
      if (plan === 'create' || !existing) {
        const marker = L.marker([row.vehicle.lat, row.vehicle.lng], {
          icon: makeLeaseIcon(row, selected),
          zIndexOffset: selected ? 1000 : row.vehicleState === 'ativo' ? 0 : 200,
        }).addTo(map);
        marker.on('click', (event) => {
          L.DomEvent.stopPropagation(event);
          onSelectRef.current(row.id);
        });
        markers.current.set(row.id, { marker, key });
        continue;
      }
      if (shouldUpdateLatLng(frozen)) {
        existing.marker.setLatLng([row.vehicle.lat, row.vehicle.lng]);
      }
      if (plan === 'rebuild') {
        existing.marker.setIcon(makeLeaseIcon(row, selected));
        existing.marker.setZIndexOffset(selected ? 1000 : row.vehicleState === 'ativo' ? 0 : 200);
        existing.key = key;
      }
    }
    for (const [id, entry] of markers.current) {
      if (keep.has(id)) {
        continue;
      }
      entry.marker.remove();
      markers.current.delete(id);
    }
  }, [frozen, map, rows, selectedId]);

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

function makeLeaseIcon(row: BookRow, selected: boolean): L.DivIcon {
  return L.divIcon({
    html: leasingMarkerHtml(row, selected),
    className: 'marker-icon',
    iconSize: [56, 44],
    iconAnchor: [28, 12],
  });
}
