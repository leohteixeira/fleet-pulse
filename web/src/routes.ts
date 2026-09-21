export type Fleet = 'rental' | 'leasing';

export function fleetForPath(pathname: string): Fleet {
  return pathname === '/carteira' || pathname.startsWith('/carteira/') ? 'leasing' : 'rental';
}

export function selectedContractId(pathname: string): string | null {
  if (!pathname.startsWith('/carteira/')) {
    return null;
  }
  const id = pathname.slice('/carteira/'.length);
  if (!id || id.includes('/')) {
    return null;
  }
  return decodeURIComponent(id);
}

export function carteiraPath(id?: string | null): string {
  return id ? `/carteira/${id}` : '/carteira';
}

export function resolveSelected<T extends { id: string }>(
  contracts: T[],
  id: string | null,
): { selected: T | null; toast: false } {
  if (!id) {
    return { selected: null, toast: false };
  }
  return { selected: contracts.find((row) => row.id === id) ?? null, toast: false };
}
