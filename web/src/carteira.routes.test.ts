import { matchPath } from 'react-router-dom';
import { describe, expect, it } from 'vitest';

import { carteiraPath, fleetForPath, resolveSelected, selectedContractId } from './routes';

describe('routes /carteira vs /', () => {
  it('maps / to rental and /carteira to leasing', () => {
    expect(matchPath({ path: '/' }, '/')).toBeTruthy();
    expect(matchPath({ path: '/carteira' }, '/')).toBeNull();
    expect(matchPath({ path: '/carteira' }, '/carteira')).toBeTruthy();
    expect(matchPath({ path: '/carteira/:id' }, '/carteira/L17')).toMatchObject({
      params: { id: 'L17' },
    });
    expect(fleetForPath('/')).toBe('rental');
    expect(fleetForPath('/carteira')).toBe('leasing');
    expect(fleetForPath('/carteira/abc')).toBe('leasing');
    expect(carteiraPath('abc')).toBe('/carteira/abc');
    expect(carteiraPath(null)).toBe('/carteira');
  });
});

describe('unknown contract id', () => {
  it('keeps the empty panel and never toasts', () => {
    const panel = resolveSelected([{ id: 'c1' }], selectedContractId('/carteira/not-a-real-id'));
    expect(panel.selected).toBeNull();
    expect(panel.toast).toBe(false);
    expect(selectedContractId('/carteira')).toBeNull();
  });
});
