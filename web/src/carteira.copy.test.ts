import { describe, expect, it } from 'vitest';

import { COPY, mapWriteError, policyCopy, runeCount } from './carteiraCopy';
import { filterRows, mergeBook, type BookRow, type Contract } from './carteiraState';
import { leasingMarkerHtml } from './carteiraMarker';
import type { Vehicle } from './state';

const vehicle: Vehicle = {
  vin: 'FPULSELSG00000017',
  displayId: 'L17',
  lat: -23.56,
  lng: -46.64,
  plate: 'ABC1D23',
  model: 'Fiat Argo',
  battery: 80,
  speed: 0,
  heading: 20,
  ignition: false,
  locked: false,
  odometer: 0,
  trip: 0,
  lastSeen: 1,
};

describe('write copy', () => {
  it('maps 422 in_day to Contrato em dia and keeps the button visible', () => {
    const refusal = mapWriteError(422, { code: 'in_day', message: 'ignored' });
    expect(refusal?.copy).toBe('Contrato em dia');
    expect(refusal?.copy).toBe(COPY.inDay);
    expect(policyCopy('in_day')).toBe('Contrato em dia');
    expect(refusal?.kind).toBe('policy');
  });

  it('maps 409 contract_busy and 429 rate limit to designed copy', () => {
    const busy = mapWriteError(409, { code: 'contract_busy', retryAfter: 12 });
    expect(busy?.copy).toBe('Aguardando intervalo entre ações · 12s');
    expect(busy?.kind).toBe('interval');

    const limited = mapWriteError(429, { code: 'rate_limited', retryAfter: 40 }, '40');
    expect(limited?.copy).toBe('Limite de requisições atingido · libera em 40s');
    expect(limited?.kind).toBe('rate');
    expect(limited?.copy.startsWith('Limite de requisições')).toBe(true);
  });

  it('counts reason runes, not UTF-16 code units', () => {
    expect(runeCount('😀😀😀😀')).toBe(4);
    expect(runeCount('áéíóúàèê')).toBe(8);
  });
});

describe('carteira filters and markers', () => {
  const contracts: Contract[] = [
    {
      id: 'c1',
      vin: 'FPULSELSG00000017',
      clientName: 'Ana',
      payerProfile: 'pontual',
      daysLate: 23,
      overdueBand: '16_30',
    },
    {
      id: 'c-rental',
      vin: 'FPULSESAO00000001',
      clientName: 'Rental',
      payerProfile: 'pontual',
      daysLate: 9,
      overdueBand: '1_15',
    },
  ];

  it('merges by VIN and drops rental plates from the book', () => {
    const rows = mergeBook(contracts, [vehicle], {}, Date.now());
    expect(rows).toHaveLength(1);
    expect(rows[0]?.displayId).toBe('L17');
    expect(rows.some((row) => row.vin.startsWith('FPULSESAO'))).toBe(false);
  });

  it('shows the dashed empty state when filters match nothing', () => {
    const rows = mergeBook(contracts, [vehicle], {}, Date.now());
    const visible = filterRows(rows, 'em_dia', 'all', '');
    expect(visible).toHaveLength(0);
  });

  it('renders ARMED with the 2.4s dashed ring and never a spinner', () => {
    const row: BookRow = {
      ...contracts[0]!,
      vehicle,
      displayId: 'L17',
      plate: vehicle.plate,
      model: vehicle.model,
      vehicleState: 'armado',
      command: { id: 'cmd', vin: vehicle.vin, action: 'block', state: 'ARMED' },
      online: true,
      lastSeen: 1,
    };
    const html = leasingMarkerHtml(row, false);
    expect(html).toContain('fp-armed');
    expect(html).toContain('L17 23d');
    expect(html).not.toContain('fp-spin');
  });
});
