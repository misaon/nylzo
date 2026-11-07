import { attempt } from '../attempt.ts';
import { browserDatabases, committed, databaseExists, openDatabase, requestResult } from './indexed-database.ts';
import { isRetained, recordFrom, storedForm } from './sent-record.ts';
import type { SentRecord } from './sent-record.ts';

const DATABASE = 'nylzo';
const RECORDS = 'sent';
const LISTING_TIMEOUT_MS = 2000;

export interface SentStore {
  list: () => Promise<SentRecord[]>;
  put: (record: SentRecord) => Promise<void>;
  markEnded: (id: string, endedAt: number) => Promise<SentRecord | undefined>;
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

function openSentStore(factory: IDBFactory | undefined): SentStore & { readonly close: () => void } {
  let database: Promise<IDBDatabase> | undefined;

  // Reading or deleting must not create the database for someone who never chose to remember a message.
  async function databaseCreated(): Promise<boolean> {
    if (database !== undefined || factory === undefined) {
      return true;
    }
    // A browser that cannot list its databases in time leaves the answer to opening, which has a timeout and an error.
    const timedOut = Promise.withResolvers<boolean>();
    const timer = setTimeout(timedOut.resolve, LISTING_TIMEOUT_MS, true);
    try {
      return await Promise.race([databaseExists(factory, DATABASE), timedOut.promise]);
    } catch {
      return true;
    } finally {
      clearTimeout(timer);
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
    const opened = await connected();
    const transaction = opened.transaction(RECORDS, mode);
    const done = committed(transaction);
    const result = action(transaction.objectStore(RECORDS));
    await Promise.all([result, done]);
    return result;
  }

  return {
    close() {
      if (database) {
        void attempt(database.then((opened) => opened.close()));
      }
      database = undefined;
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
    async markEnded(id, endedAt) {
      return (await databaseCreated())
        ? withRecords('readwrite', async (records) => endIfStored(records, id, endedAt))
        : undefined;
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

export function browserSentStore(factory = browserDatabases()): SentStore {
  return openSentStore(factory);
}

export async function purgeSentRecords(factory: IDBFactory | undefined, now: number): Promise<void> {
  try {
    if (await databaseExists(factory, DATABASE)) {
      const store = openSentStore(factory);
      try {
        await retainedRecords(store, now);
      } finally {
        store.close();
      }
    }
  } catch {}
}

async function endIfStored(records: IDBObjectStore, id: string, endedAt: number): Promise<SentRecord | undefined> {
  const record = recordFrom(await requestResult(records.get(id)));
  if (!record || record.endedAt !== undefined) {
    return record;
  }
  const ended = { ...record, endedAt };
  await requestResult(records.put(storedForm(ended)));
  return ended;
}
