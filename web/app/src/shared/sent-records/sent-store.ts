import { browserDatabases, committed, databaseExists, openDatabase, requestResult } from './indexed-database.ts';
import { isRetained, recordFrom, type SentRecord, storedForm } from './sent-record.ts';

const DATABASE = 'nylzo';
const RECORDS = 'sent';

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

export function browserSentStore(factory = browserDatabases()): SentStore {
  let database: Promise<IDBDatabase> | undefined;

  async function withRecords<Result>(
    mode: IDBTransactionMode,
    action: (records: IDBObjectStore) => Promise<Result>,
  ): Promise<Result> {
    database ??= openDatabase(factory, DATABASE, createRecords);
    const opened = await database;
    const transaction = opened.transaction(RECORDS, mode);
    const done = committed(transaction);
    const result = action(transaction.objectStore(RECORDS));
    await Promise.all([result, done]);
    return result;
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

  return {
    async list() {
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
      return withRecords('readwrite', async (records) => endIfStored(records, id, endedAt));
    },
    async remove(id) {
      await withRecords('readwrite', async (records) => requestResult(records.delete(id)));
    },
    async clear() {
      await withRecords('readwrite', async (records) => requestResult(records.clear()));
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
  } catch {
    return;
  }
}

export async function rememberSent(store: SentStore, record: SentRecord, timeoutMs: number): Promise<boolean> {
  const saving = saved(store, record);
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<'timeout'>((resolve) => {
    timer = setTimeout(resolve, timeoutMs, 'timeout');
  });
  const result = await Promise.race([saving, timedOut]);
  clearTimeout(timer);
  if (result !== 'timeout') {
    return result;
  }
  void removeLateRecord(store, record.id, saving);
  return false;
}

export async function purgeSentRecords(factory: IDBFactory | undefined, now: number): Promise<void> {
  try {
    if (await databaseExists(factory, DATABASE)) {
      await retainedRecords(browserSentStore(factory), now);
    }
  } catch {
    return;
  }
}
