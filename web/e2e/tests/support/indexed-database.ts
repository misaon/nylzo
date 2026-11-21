import type { Page } from '@playwright/test';

declare global {
  var nylzoCompletedWrites: number | undefined;
}

export interface SentRecord {
  readonly id: string;
  readonly endedAt?: number | undefined;
  readonly fields: string[];
  readonly publicKeyLength: number;
  readonly privateKey: {
    readonly algorithm: string;
    readonly extractable: boolean;
    readonly usages: string[];
    readonly exportable: boolean;
  };
}

interface StoredRecord {
  readonly id: string;
  readonly endedAt?: number | undefined;
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array;
}

export const EXPIRED_RECORD_ID = 'AAAAAAAAAAAAAAAAAAAAAA';

export async function databaseNames(page: Page): Promise<string[]> {
  return page.evaluate(async () => {
    const databases = await indexedDB.databases();
    return databases.map(({ name = '' }) => name);
  });
}

export function sentRecords(page: Page): Promise<SentRecord[]> {
  return page.evaluate(readSentRecords);
}

export function storeExpiredRecord(page: Page): Promise<void> {
  return page.evaluate(writeExpiredRecord, EXPIRED_RECORD_ID);
}

export async function countCompletedWrites(page: Page): Promise<() => Promise<number>> {
  await page.addInitScript(() => {
    let completed = 0;
    Object.defineProperty(globalThis, 'nylzoCompletedWrites', { get: () => completed });
    const transaction: unknown = Object.getOwnPropertyDescriptor(IDBDatabase.prototype, 'transaction')?.value;
    if (typeof transaction !== 'function') {
      return;
    }
    Object.defineProperty(IDBDatabase.prototype, 'transaction', {
      value(this: IDBDatabase, ...parameters: Parameters<IDBDatabase['transaction']>): unknown {
        const opened: unknown = Reflect.apply(transaction, this, parameters);
        if (opened instanceof IDBTransaction && opened.mode === 'readwrite') {
          opened.addEventListener('complete', () => {
            completed += 1;
          });
        }
        return opened;
      },
    });
  });
  return () => page.evaluate(() => globalThis.nylzoCompletedWrites ?? 0);
}

async function readSentRecords(): Promise<SentRecord[]> {
  const databases = await indexedDB.databases();
  if (databases.every(({ name }) => name !== 'nylzo')) {
    return [];
  }
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const opening = indexedDB.open('nylzo');
    opening.addEventListener('success', () => {
      resolve(opening.result);
    });
    opening.addEventListener('error', () => {
      reject(new Error('the sent store does not open'));
    });
  });
  try {
    const stored = await new Promise<unknown[]>((resolve, reject) => {
      const reading = database.transaction('sent').objectStore('sent').getAll();
      reading.addEventListener('success', () => {
        resolve(reading.result);
      });
      reading.addEventListener('error', () => {
        reject(new Error('the sent store does not read'));
      });
    });
    // oxlint-disable-next-line unicorn/consistent-function-scoping -- Playwright serializes this closure into the browser; module-scope helpers are unavailable there.
    const isStoredRecord = (value: unknown): value is StoredRecord =>
      typeof value === 'object' &&
      value !== null &&
      'id' in value &&
      typeof value.id === 'string' &&
      (!('endedAt' in value) || value.endedAt === undefined || typeof value.endedAt === 'number') &&
      'privateKey' in value &&
      value.privateKey instanceof CryptoKey &&
      'publicKey' in value &&
      value.publicKey instanceof Uint8Array;
    // oxlint-disable-next-line unicorn/consistent-function-scoping -- Playwright serializes this closure into the browser; module-scope helpers are unavailable there.
    const exportable = async (key: CryptoKey) => {
      try {
        await crypto.subtle.exportKey('pkcs8', key);
        return true;
      } catch {
        return false;
      }
    };
    return await Promise.all(
      stored.map(async (record) => {
        if (!isStoredRecord(record)) {
          throw new TypeError('the sent store holds a record of an unknown shape');
        }
        return {
          id: record.id,
          endedAt: record.endedAt,
          fields: Object.keys(record).toSorted((left, right) => left.localeCompare(right)),
          publicKeyLength: record.publicKey.length,
          privateKey: {
            algorithm: record.privateKey.algorithm.name,
            extractable: record.privateKey.extractable,
            usages: record.privateKey.usages,
            exportable: await exportable(record.privateKey),
          },
        };
      }),
    );
  } finally {
    database.close();
  }
}

async function writeExpiredRecord(id: string): Promise<void> {
  const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
  const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey));
  const expiresAt = Date.now() - 8 * 24 * 3_600_000;
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const opening = indexedDB.open('nylzo');
    opening.addEventListener('success', () => {
      resolve(opening.result);
    });
    opening.addEventListener('error', () => {
      reject(new Error('the sent store does not open'));
    });
  });
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction('sent', 'readwrite');
      transaction
        .objectStore('sent')
        .put({ id, createdAt: expiresAt - 3_600_000, expiresAt, privateKey: pair.privateKey, publicKey });
      transaction.addEventListener('complete', () => {
        resolve();
      });
      transaction.addEventListener('error', () => {
        reject(new Error('the sent store does not write'));
      });
    });
  } finally {
    database.close();
  }
}
