import { attempt } from '../attempt.ts';
import { browserDatabases, committed, databaseExists, openDatabase, requestResult } from './indexed-database.ts';
import { isRetained, recordFrom, retainedUntil, storedForm } from './sent-record.ts';
import type { SentRecord } from './sent-record.ts';

const DATABASE = 'nylzo';
const RECORDS = 'sent';
const LISTING_TIMEOUT_MS = 2000;
const RECHECK_MS = 60 * 60 * 1000;

export interface SentStore {
  list: () => Promise<SentRecord[]>;
  put: (record: SentRecord) => Promise<void>;
  remove: (id: string) => Promise<void>;
  clear: () => Promise<void>;
}

function createRecords(database: IDBDatabase): void {
  database.createObjectStore(RECORDS, { keyPath: 'id' });
}

function storedId(value: unknown): string | undefined {
  if (typeof value !== 'object' || value === null || !('id' in value)) {
    return undefined;
  }
  return typeof value.id === 'string' ? value.id : undefined;
}

// A listing that hangs counts as no database, so nothing creates it.
async function databaseListed(factory: IDBFactory, ifListingFails: boolean): Promise<boolean> {
  const timedOut = Promise.withResolvers<boolean>();
  const timer = setTimeout(timedOut.resolve, LISTING_TIMEOUT_MS, false);
  try {
    return await Promise.race([databaseExists(factory, DATABASE), timedOut.promise]);
  } catch {
    return ifListingFails;
  } finally {
    clearTimeout(timer);
  }
}

function openSentStore(factory: IDBFactory | undefined): SentStore & { readonly close: () => void } {
  let database: Promise<IDBDatabase> | undefined;
  let closed = false;
  let active = 0;

  // Reading or deleting must not create the database for someone who never chose to remember a message; a failed listing leaves the answer to opening.
  async function databaseCreated(): Promise<boolean> {
    return database !== undefined || factory === undefined || databaseListed(factory, true);
  }

  // After close(), a late operation still runs on its own connection, which closes when the last operation ends.
  function release(): void {
    if (closed && active === 0 && database) {
      void attempt(database.then((opened) => opened.close()));
      database = undefined;
    }
  }

  async function connect(): Promise<IDBDatabase> {
    const opened = await openDatabase(factory, DATABASE, createRecords);
    // Another tab that deletes or upgrades the database must not wait for this one.
    opened.addEventListener('versionchange', () => {
      opened.close();
      database = undefined;
    });
    opened.addEventListener('close', () => {
      database = undefined;
    });
    return opened;
  }

  async function connected(): Promise<IDBDatabase> {
    const opening = (database ??= connect());
    try {
      return await opening;
    } catch (error) {
      if (database === opening) {
        database = undefined;
      }
      throw error;
    }
  }

  async function withRecords<Result>(
    mode: IDBTransactionMode,
    action: (records: IDBObjectStore) => Promise<Result>,
  ): Promise<Result> {
    active++;
    try {
      const opened = await connected();
      const transaction = opened.transaction(RECORDS, mode);
      const done = committed(transaction);
      const result = action(transaction.objectStore(RECORDS));
      await Promise.all([result, done]);
      return await result;
    } finally {
      active--;
      release();
    }
  }

  return {
    close() {
      closed = true;
      release();
    },
    async list() {
      if (!(await databaseCreated())) {
        return [];
      }
      const values = await withRecords('readonly', async (records) => requestResult(records.getAll()));
      const kept: SentRecord[] = [];
      for (const value of values) {
        const record = recordFrom(value);
        const brokenId = record ? undefined : storedId(value);
        if (record) {
          kept.push(record);
        } else if (brokenId !== undefined) {
          await withRecords('readwrite', async (records) => requestResult(records.delete(brokenId)));
        }
      }
      return kept;
    },
    async put(record) {
      const stored = storedForm(record);
      await withRecords('readwrite', async (records) => requestResult(records.put(stored)));
    },
    async remove(id) {
      if (await databaseCreated()) {
        await withRecords('readwrite', async (records) => requestResult(records.delete(id)));
      }
    },
    async clear() {
      if (await databaseCreated()) {
        await withRecords('readwrite', async (records) => requestResult(records.clear()));
      }
    },
  };
}

export async function retainedRecords(store: SentStore, now: number): Promise<SentRecord[]> {
  const records = await store.list();
  const kept: SentRecord[] = [];
  for (const record of records) {
    if (isRetained(record, now)) {
      kept.push(record);
    } else {
      await store.remove(record.id);
    }
  }
  return kept.toSorted((first, second) => second.createdAt - first.createdAt);
}

async function saved(store: SentStore, record: SentRecord): Promise<boolean> {
  try {
    await store.put(record);
    return true;
  } catch {
    return false;
  }
}

async function removeLateRecord(store: SentStore, id: string, saving: Promise<boolean>): Promise<void> {
  try {
    if (await saving) {
      await store.remove(id);
    }
  } catch {}
}

export async function rememberSent(store: SentStore, record: SentRecord, timeoutMs: number): Promise<boolean> {
  const saving = saved(store, record);
  const timedOut = Promise.withResolvers<'timeout'>();
  const timer = setTimeout(timedOut.resolve, timeoutMs, 'timeout');
  const result = await Promise.race([saving, timedOut.promise]);
  clearTimeout(timer);
  if (result !== 'timeout') {
    return result;
  }
  void removeLateRecord(store, record.id, saving);
  return false;
}

export function browserSentStore(factory = browserDatabases()): SentStore & { readonly close: () => void } {
  return openSentStore(factory);
}

function nextPurgeAt(records: readonly SentRecord[]): number | undefined {
  const deadlines = records.map((record) => retainedUntil(record));
  return deadlines.length === 0 ? undefined : Math.min(...deadlines);
}

export async function purgeSentRecords(factory: IDBFactory | undefined, now: number): Promise<number | undefined> {
  try {
    if (factory && (await databaseListed(factory, false))) {
      const store = openSentStore(factory);
      try {
        return nextPurgeAt(await retainedRecords(store, now));
      } finally {
        store.close();
      }
    }
  } catch {}
  return undefined;
}

// No page can delete anything while none is open, so an open page checks again when a record is due and at least hourly.
export async function keepPurgingSentRecords(factory: IDBFactory | undefined, now: () => number): Promise<void> {
  const next = await purgeSentRecords(factory, now());
  const wait = Math.min((next ?? Infinity) - now(), RECHECK_MS);
  setTimeout(() => void keepPurgingSentRecords(factory, now), Math.max(wait, 0));
}
