import { PUBLIC_KEY_LEN, parseId, type RequestKey } from '@nylzo/core';

import { ApiError, type Api, type Status } from './api.ts';

export const RETENTION_MS = 7 * 24 * 60 * 60 * 1000;

const DATABASE = 'nylzo';
const DATABASE_VERSION = 1;
const RECORDS = 'sent';

export interface SentRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly manageKey: RequestKey;
}

export interface StoredRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export interface SentStore {
  list(): Promise<SentRecord[]>;
  put(record: SentRecord): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
}

export class ExtractableKeyError extends Error {
  override readonly name = 'ExtractableKeyError';
  constructor() {
    super('a manage key must not be extractable');
  }
}

export class SentStoreUnavailableError extends Error {
  override readonly name = 'SentStoreUnavailableError';
  constructor() {
    super('this browser does not let the page store sent messages');
  }
}

function validId(id: unknown): id is string {
  if (typeof id !== 'string') {
    return false;
  }
  try {
    parseId(id);
    return true;
  } catch {
    return false;
  }
}

function isSigningKey(key: unknown): key is CryptoKey {
  return key instanceof CryptoKey && key.type === 'private' && key.algorithm.name === 'Ed25519' && key.usages.includes('sign');
}

export function storedForm(record: SentRecord): StoredRecord {
  if (record.manageKey.privateKey.extractable) {
    throw new ExtractableKeyError();
  }
  return {
    id: record.id,
    createdAt: record.createdAt,
    expiresAt: record.expiresAt,
    privateKey: record.manageKey.privateKey,
    publicKey: record.manageKey.publicKey,
  };
}

export function recordFrom(value: unknown): SentRecord | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined;
  }
  const { id, createdAt, expiresAt, privateKey, publicKey } = value as Partial<Record<keyof StoredRecord, unknown>>;
  if (
    !validId(id) ||
    typeof createdAt !== 'number' ||
    !Number.isFinite(createdAt) ||
    typeof expiresAt !== 'number' ||
    !Number.isFinite(expiresAt) ||
    !isSigningKey(privateKey) ||
    privateKey.extractable ||
    !(publicKey instanceof Uint8Array) ||
    publicKey.length !== PUBLIC_KEY_LEN
  ) {
    return undefined;
  }
  return { id, createdAt, expiresAt, manageKey: { privateKey, publicKey: new Uint8Array(publicKey) } };
}

export function isRetained(record: Pick<SentRecord, 'expiresAt'>, now: number): boolean {
  return now < record.expiresAt + RETENTION_MS;
}

export async function retainedRecords(store: SentStore, now: number): Promise<SentRecord[]> {
  const kept: SentRecord[] = [];
  for (const record of await store.list()) {
    if (isRetained(record, now)) {
      kept.push(record);
    } else {
      await store.remove(record.id);
    }
  }
  return kept.sort((a, b) => b.createdAt - a.createdAt);
}

function settle<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new SentStoreUnavailableError());
  });
}

function committed(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new SentStoreUnavailableError());
    transaction.onabort = () => reject(transaction.error ?? new SentStoreUnavailableError());
  });
}

function openDatabase(factory: IDBFactory | undefined): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (!factory) {
      reject(new SentStoreUnavailableError());
      return;
    }
    let request: IDBOpenDBRequest;
    try {
      request = factory.open(DATABASE, DATABASE_VERSION);
    } catch {
      reject(new SentStoreUnavailableError());
      return;
    }
    request.onupgradeneeded = () => {
      request.result.createObjectStore(RECORDS, { keyPath: 'id' });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(new SentStoreUnavailableError());
    request.onblocked = () => reject(new SentStoreUnavailableError());
  });
}

export function indexedDbSentStore(factory: IDBFactory | undefined = globalThis.indexedDB): SentStore {
  let database: Promise<IDBDatabase> | undefined;

  async function run<T>(mode: IDBTransactionMode, action: (records: IDBObjectStore) => IDBRequest<T>): Promise<T> {
    database ??= openDatabase(factory);
    const transaction = (await database).transaction(RECORDS, mode);
    const result = settle(action(transaction.objectStore(RECORDS)));
    await committed(transaction);
    return result;
  }

  return {
    async list() {
      const records: SentRecord[] = [];
      for (const value of await run('readonly', (records) => records.getAll())) {
        const record = recordFrom(value);
        if (record) {
          records.push(record);
        } else if (typeof value === 'object' && value !== null && typeof (value as { id?: unknown }).id === 'string') {
          const id = (value as { id: string }).id;
          await run('readwrite', (stored) => stored.delete(id));
        }
      }
      return records;
    },
    async put(record) {
      const stored = storedForm(record);
      await run('readwrite', (records) => records.put(stored));
    },
    async remove(id) {
      await run('readwrite', (records) => records.delete(id));
    },
    async clear() {
      await run('readwrite', (records) => records.clear());
    },
  };
}

export type ItemState =
  | { readonly kind: 'loading' }
  | { readonly kind: 'loaded'; readonly status: Status }
  | { readonly kind: 'unknown' }
  | { readonly kind: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly kind: 'failed' };

export interface SentItem {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly state: ItemState;
}

export type SentView =
  | { readonly view: 'loading' }
  | { readonly view: 'unavailable' }
  | { readonly view: 'list'; readonly items: readonly SentItem[] };

export type CancelOutcome =
  | { readonly outcome: 'cancelled' }
  | { readonly outcome: 'ended' }
  | { readonly outcome: 'unknown' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

export interface SentDeps {
  readonly api: Api;
  readonly store: SentStore;
  readonly now: () => number;
}

export interface SentPage {
  load(): Promise<void>;
  refresh(id: string): Promise<void>;
  cancel(id: string): Promise<CancelOutcome>;
  forget(id: string): Promise<void>;
  forgetAll(): Promise<void>;
}

export function isCancellable(state: ItemState): boolean {
  return state.kind === 'loaded' && state.status.endedAt === undefined && (state.status.state === 'available' || state.status.state === 'opened');
}

function failedState(error: unknown): ItemState {
  if (error instanceof ApiError) {
    if (error.kind === 'not_found') {
      return { kind: 'unknown' };
    }
    if (error.kind === 'rate_limited' || error.kind === 'unavailable') {
      return { kind: 'retry_later', retryAfter: error.retryAfter };
    }
  }
  return { kind: 'failed' };
}

export function createSentPage(deps: SentDeps, notify: (view: SentView) => void): SentPage {
  let records: SentRecord[] = [];
  let states = new Map<string, ItemState>();
  let generation = 0;
  const cancelling = new Set<string>();

  function show(): void {
    notify({
      view: 'list',
      items: records.map((record) => ({
        id: record.id,
        createdAt: record.createdAt,
        expiresAt: record.expiresAt,
        state: states.get(record.id) ?? { kind: 'loading' },
      })),
    });
  }

  function setState(id: string, state: ItemState): void {
    if (records.some((record) => record.id === id)) {
      states.set(id, state);
      show();
    }
  }

  async function fetchStatus(record: SentRecord): Promise<ItemState> {
    try {
      return { kind: 'loaded', status: await deps.api.status(parseId(record.id), record.manageKey) };
    } catch (error) {
      return failedState(error);
    }
  }

  async function load(): Promise<void> {
    const current = ++generation;
    notify({ view: 'loading' });
    try {
      records = await retainedRecords(deps.store, deps.now());
    } catch {
      records = [];
      notify({ view: 'unavailable' });
      return;
    }
    states = new Map();
    show();
    for (const [index, record] of records.entries()) {
      const state = await fetchStatus(record);
      if (current !== generation) {
        return;
      }
      setState(record.id, state);
      if (state.kind === 'retry_later') {
        for (const rest of records.slice(index + 1)) {
          states.set(rest.id, state);
        }
        show();
        return;
      }
    }
  }

  async function refresh(id: string): Promise<void> {
    const record = records.find((candidate) => candidate.id === id);
    if (!record) {
      return;
    }
    setState(id, { kind: 'loading' });
    setState(id, await fetchStatus(record));
  }

  async function cancel(id: string): Promise<CancelOutcome> {
    const record = records.find((candidate) => candidate.id === id);
    if (!record || cancelling.has(id)) {
      return { outcome: 'ignored' };
    }
    cancelling.add(id);
    let outcome: CancelOutcome;
    try {
      await deps.api.cancel(parseId(id), record.manageKey);
      outcome = { outcome: 'cancelled' };
    } catch (error) {
      if (error instanceof ApiError && error.kind === 'gone') {
        outcome = { outcome: 'ended' };
      } else if (error instanceof ApiError && error.kind === 'not_found') {
        outcome = { outcome: 'unknown' };
      } else if (error instanceof ApiError && (error.kind === 'rate_limited' || error.kind === 'unavailable')) {
        outcome = { outcome: 'retry_later', retryAfter: error.retryAfter };
      } else {
        outcome = { outcome: 'failed' };
      }
    } finally {
      cancelling.delete(id);
    }
    if (outcome.outcome === 'unknown') {
      setState(id, { kind: 'unknown' });
    } else if (outcome.outcome !== 'retry_later' && outcome.outcome !== 'failed') {
      await refresh(id);
    }
    return outcome;
  }

  async function forget(id: string): Promise<void> {
    await deps.store.remove(id);
    records = records.filter((record) => record.id !== id);
    states.delete(id);
    show();
  }

  async function forgetAll(): Promise<void> {
    generation++;
    await deps.store.clear();
    records = [];
    states = new Map();
    show();
  }

  return { load, refresh, cancel, forget, forgetAll };
}
