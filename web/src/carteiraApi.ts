import { mapWriteError, type WriteRefusal } from './carteiraCopy';
import type { ClockSnapshot, Contract, ContractDetail } from './carteiraState';
import type { Vehicle } from './state';

export const CLOCK_ERROR = 'Não foi possível carregar o relógio.';
export const BOOK_ERROR = 'Não foi possível carregar a carteira.';

export type WriteAction = 'notify' | 'block' | 'cancel' | 'pay';

export type WriteOutcome =
  | { ok: true; status: number; body: unknown }
  | { ok: false; refusal: WriteRefusal; status: number };

export async function loadClock(signal?: AbortSignal): Promise<ClockSnapshot> {
  const body = await getJSON('/api/clock', signal, CLOCK_ERROR);
  if (!isClock(body)) {
    throw new Error(CLOCK_ERROR);
  }
  return body;
}

export async function loadContracts(signal?: AbortSignal): Promise<Contract[]> {
  const body = await getJSON('/api/contracts', signal, BOOK_ERROR);
  if (!isContractList(body)) {
    throw new Error(BOOK_ERROR);
  }
  return body.contracts;
}

export async function loadLeasingVehicles(signal?: AbortSignal): Promise<Vehicle[]> {
  const body = await getJSON('/api/leasing/vehicles', signal, BOOK_ERROR);
  if (!isVehicleList(body)) {
    throw new Error(BOOK_ERROR);
  }
  return body.vehicles;
}

export async function loadContractDetail(
  id: string,
  signal?: AbortSignal,
): Promise<ContractDetail | null> {
  let response: Response;
  try {
    response = await fetch(`/api/contracts/${id}`, { signal });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    return null;
  }
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    return null;
  }
  let body: unknown;
  try {
    body = (await response.json()) as unknown;
  } catch {
    return null;
  }
  return isDetail(body) ? body : null;
}

export async function postNotify(id: string, daysLate: number): Promise<WriteOutcome> {
  return postWrite(`/api/contracts/${id}/notify`, undefined, daysLate);
}

export async function postBlock(id: string, reason: string, daysLate: number): Promise<WriteOutcome> {
  return postWrite(`/api/contracts/${id}/block`, { reason }, daysLate);
}

export async function postCancel(id: string, daysLate: number): Promise<WriteOutcome> {
  return postWrite(`/api/contracts/${id}/block/cancel`, undefined, daysLate);
}

export async function postPayment(id: string, daysLate: number): Promise<WriteOutcome> {
  return postWrite(`/api/contracts/${id}/payments`, undefined, daysLate);
}

async function postWrite(url: string, body: unknown, daysLate: number): Promise<WriteOutcome> {
  let response: Response;
  try {
    response = await fetch(url, {
      method: 'POST',
      headers: {
        'Idempotency-Key': crypto.randomUUID(),
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    return {
      ok: false,
      status: 0,
      refusal: { kind: 'policy', code: 'network', copy: BOOK_ERROR, retryAfter: 0 },
    };
  }
  let parsed: unknown = null;
  try {
    parsed = (await response.json()) as unknown;
  } catch {
    parsed = null;
  }
  if (response.ok) {
    return { ok: true, status: response.status, body: parsed };
  }
  const refusal = mapWriteError(response.status, parsed, response.headers.get('Retry-After'), daysLate);
  if (refusal) {
    return { ok: false, status: response.status, refusal };
  }
  return {
    ok: false,
    status: response.status,
    refusal: { kind: 'policy', code: 'error', copy: BOOK_ERROR, retryAfter: 0 },
  };
}

async function getJSON(url: string, signal: AbortSignal | undefined, error: string): Promise<unknown> {
  let response: Response;
  try {
    response = await fetch(url, { signal });
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      throw err;
    }
    throw new Error(error);
  }
  if (!response.ok) {
    throw new Error(error);
  }
  try {
    return (await response.json()) as unknown;
  } catch {
    throw new Error(error);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function isClock(value: unknown): value is ClockSnapshot {
  if (!isRecord(value)) {
    return false;
  }
  return (
    typeof value.simulated === 'string' &&
    typeof value.real === 'string' &&
    typeof value.rate === 'string' &&
    typeof value.multiplier === 'number'
  );
}

function isContractList(value: unknown): value is { contracts: Contract[] } {
  if (!isRecord(value) || !Array.isArray(value.contracts)) {
    return false;
  }
  return value.contracts.every((item) => {
    return isRecord(item) && typeof item.id === 'string' && typeof item.vin === 'string';
  });
}

function isVehicleList(value: unknown): value is { vehicles: Vehicle[] } {
  if (!isRecord(value) || !Array.isArray(value.vehicles)) {
    return false;
  }
  return value.vehicles.every((item) => {
    return isRecord(item) && typeof item.vin === 'string';
  });
}

function isDetail(value: unknown): value is ContractDetail {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.vin !== 'string') {
    return false;
  }
  return Array.isArray(value.installments) && Array.isArray(value.audit);
}
