import { PUBLIC_KEY_LEN, parseId, type RequestKey } from '@nylzo/core';

import { ApiError, type Api, type Status } from './api.ts';

const DAY_MS = 24 * 60 * 60 * 1000;
export const RETENTION_DAYS = 7;
export const RETENTION_MS = RETENTION_DAYS * DAY_MS;
const MAX_LIFETIME_MS = 31 * DAY_MS;
const MAX_TIME_MS = 8.64e15;

const DATABASE = 'nylzo';
const DATABASE_VERSION = 1;
const RECORDS = 'sent';

export interface SentRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly endedAt?: number | undefined;
  readonly manageKey: RequestKey;
}

export interface StoredRecord {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly endedAt?: number;
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export interface SentStore {
  list(): Promise<SentRecord[]>;
  put(record: SentRecord): Promise<void>;
  markEnded(id: string, endedAt: number): Promise<SentRecord | undefined>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
}

export class ExtractableKeyError extends Error {
  override readonly name = 'ExtractableKeyError';
  constructor() {
    super('a manage key must not be extractable');
  }
}

export class InvalidRecordError extends Error {
  override readonly name = 'InvalidRecordError';
  constructor() {
    super('a sent record has impossible times or keys');
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

function validTime(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= MAX_TIME_MS;
}

function validTimes(createdAt: unknown, expiresAt: unknown, endedAt: unknown): boolean {
  return (
    validTime(createdAt) &&
    validTime(expiresAt) &&
    createdAt <= expiresAt &&
    expiresAt - createdAt <= MAX_LIFETIME_MS &&
    (endedAt === undefined || (validTime(endedAt) && endedAt <= expiresAt + MAX_LIFETIME_MS))
  );
}

function isSigningKey(key: unknown): key is CryptoKey {
  return key instanceof CryptoKey && key.type === 'private' && key.algorithm.name === 'Ed25519' && key.usages.includes('sign');
}

export function storedForm(record: SentRecord): StoredRecord {
  if (record.manageKey.privateKey.extractable) {
    throw new ExtractableKeyError();
  }
  if (!validId(record.id) || !validTimes(record.createdAt, record.expiresAt, record.endedAt)) {
    throw new InvalidRecordError();
  }
  return {
    id: record.id,
    createdAt: record.createdAt,
    expiresAt: record.expiresAt,
    ...(record.endedAt === undefined ? {} : { endedAt: record.endedAt }),
    privateKey: record.manageKey.privateKey,
    publicKey: record.manageKey.publicKey,
  };
}

export function recordFrom(value: unknown): SentRecord | undefined {
  if (typeof value !== 'object' || value === null) {
    return undefined;
  }
  const { id, createdAt, expiresAt, endedAt, privateKey, publicKey } = value as Partial<Record<keyof StoredRecord, unknown>>;
  if (
    !validId(id) ||
    !validTimes(createdAt, expiresAt, endedAt) ||
    !validTime(createdAt) ||
    !validTime(expiresAt) ||
    !isSigningKey(privateKey) ||
    privateKey.extractable ||
    !(publicKey instanceof Uint8Array) ||
    publicKey.length !== PUBLIC_KEY_LEN
  ) {
    return undefined;
  }
  return {
    id,
    createdAt,
    expiresAt,
    ...(validTime(endedAt) ? { endedAt } : {}),
    manageKey: { privateKey, publicKey: new Uint8Array(publicKey) },
  };
}

export function isRetained(record: Pick<SentRecord, 'expiresAt' | 'endedAt'>, now: number): boolean {
  return now < Math.min(record.expiresAt, record.endedAt ?? Number.POSITIVE_INFINITY) + RETENTION_MS;
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

  async function run<T>(mode: IDBTransactionMode, action: (records: IDBObjectStore) => Promise<T>): Promise<T> {
    database ??= openDatabase(factory);
    const transaction = (await database).transaction(RECORDS, mode);
    const done = committed(transaction);
    const result = action(transaction.objectStore(RECORDS));
    await Promise.all([result, done]);
    return result;
  }

  async function endIfStored(records: IDBObjectStore, id: string, endedAt: number): Promise<SentRecord | undefined> {
    const record = recordFrom(await settle(records.get(id)));
    if (!record || record.endedAt !== undefined) {
      return record;
    }
    const ended = { ...record, endedAt };
    await settle(records.put(storedForm(ended)));
    return ended;
  }

  return {
    async list() {
      const records: SentRecord[] = [];
      for (const value of await run('readonly', (records) => settle(records.getAll()))) {
        const record = recordFrom(value);
        if (record) {
          records.push(record);
        } else if (typeof value === 'object' && value !== null && typeof (value as { id?: unknown }).id === 'string') {
          const id = (value as { id: string }).id;
          await run('readwrite', (stored) => settle(stored.delete(id)));
        }
      }
      return records;
    },
    async put(record) {
      const stored = storedForm(record);
      await run('readwrite', (records) => settle(records.put(stored)));
    },
    async markEnded(id, endedAt) {
      return run('readwrite', (records) => endIfStored(records, id, endedAt));
    },
    async remove(id) {
      await run('readwrite', (records) => settle(records.delete(id)));
    },
    async clear() {
      await run('readwrite', (records) => settle(records.clear()));
    },
  };
}

export async function rememberSent(store: SentStore, record: SentRecord, timeoutMs: number): Promise<boolean> {
  const saving = store.put(record).then(
    () => true,
    () => false,
  );
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<undefined>((resolve) => {
    timer = setTimeout(resolve, timeoutMs, undefined);
  });
  const saved = await Promise.race([saving, timedOut]);
  clearTimeout(timer);
  if (saved !== undefined) {
    return saved;
  }
  saving.then((late) => (late ? store.remove(record.id) : undefined)).catch(() => undefined);
  return false;
}

export async function purgeSentRecords(factory: IDBFactory | undefined, now: number): Promise<void> {
  if (!factory || typeof factory.databases !== 'function') {
    return;
  }
  const databases = await factory.databases();
  if (databases.some((database) => database.name === DATABASE)) {
    await retainedRecords(indexedDbSentStore(factory), now);
  }
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
  readonly ended: boolean;
  readonly cancellable: boolean;
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

export function assess(state: ItemState, expiresAt: number, now: number): Pick<SentItem, 'ended' | 'cancellable'> {
  if (state.kind !== 'loaded') {
    return { ended: false, cancellable: false };
  }
  const { status } = state;
  const expires = status.expiresAt === undefined ? expiresAt : Date.parse(status.expiresAt);
  const ended = status.endedAt !== undefined || (status.state !== 'available' && status.state !== 'opened') || expires <= now;
  return { ended, cancellable: !ended };
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

function cancelOutcome(error: unknown): CancelOutcome {
  if (error instanceof ApiError) {
    switch (error.kind) {
      case 'gone':
        return { outcome: 'ended' };
      case 'not_found':
        return { outcome: 'unknown' };
      case 'rate_limited':
      case 'unavailable':
        return { outcome: 'retry_later', retryAfter: error.retryAfter };
      default:
        return { outcome: 'failed' };
    }
  }
  return { outcome: 'failed' };
}

export function createSentPage(deps: SentDeps, notify: (view: SentView) => void): SentPage {
  let records: SentRecord[] = [];
  let states = new Map<string, ItemState>();
  let generation = 0;
  const cancelling = new Set<string>();

  function show(): void {
    const now = deps.now();
    notify({
      view: 'list',
      items: records.map((record) => {
        const state = states.get(record.id) ?? { kind: 'loading' };
        return { id: record.id, createdAt: record.createdAt, expiresAt: record.expiresAt, state, ...assess(state, record.expiresAt, now) };
      }),
    });
  }

  function listed(id: string): boolean {
    return records.some((record) => record.id === id);
  }

  function setState(id: string, state: ItemState): void {
    if (listed(id)) {
      states.set(id, state);
      show();
    }
  }

  function drop(id: string): void {
    records = records.filter((record) => record.id !== id);
    states.delete(id);
    show();
  }

  async function stored(id: string): Promise<SentRecord | undefined> {
    const found = (await deps.store.list()).find((record) => record.id === id);
    if (!found) {
      drop(id);
    }
    return found;
  }

  async function rememberEnd(record: SentRecord, status: Status): Promise<void> {
    if (status.endedAt === undefined || record.endedAt !== undefined) {
      return;
    }
    const ended = await deps.store.markEnded(record.id, Date.parse(status.endedAt)).catch(() => undefined);
    if (ended) {
      records = records.map((candidate) => (candidate.id === record.id ? ended : candidate));
    }
  }

  async function fetchStatus(id: string): Promise<ItemState | undefined> {
    try {
      const record = await stored(id);
      if (!record) {
        return undefined;
      }
      const status = await deps.api.status(parseId(id), record.manageKey);
      await rememberEnd(record, status);
      return { kind: 'loaded', status };
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
    for (const { id } of [...records]) {
      if (current !== generation) {
        return;
      }
      const state = await fetchStatus(id);
      if (current !== generation || state === undefined) {
        continue;
      }
      setState(id, state);
      if (state.kind === 'retry_later') {
        for (const record of records) {
          if (!states.has(record.id)) {
            states.set(record.id, state);
          }
        }
        show();
        return;
      }
    }
  }

  async function refresh(id: string): Promise<void> {
    if (!listed(id)) {
      return;
    }
    setState(id, { kind: 'loading' });
    const state = await fetchStatus(id);
    if (state) {
      setState(id, state);
    }
  }

  async function cancel(id: string): Promise<CancelOutcome> {
    if (!listed(id) || cancelling.has(id)) {
      return { outcome: 'ignored' };
    }
    cancelling.add(id);
    let outcome: CancelOutcome;
    try {
      const record = await stored(id);
      if (!record) {
        return { outcome: 'ignored' };
      }
      await deps.api.cancel(parseId(id), record.manageKey);
      outcome = { outcome: 'cancelled' };
    } catch (error) {
      outcome = cancelOutcome(error);
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
    drop(id);
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
