import { encodeBase64url, MAX_OBJECT_BYTES, parseId, transferSeconds, utf8 } from '@nylzo/core';

import { browserDatabases, committed, databaseExists, openDatabase } from './indexed-database.ts';

const DATABASE = 'nylzo-resume';
const RECORDS = 'keys';
const LIFETIME_MS = (transferSeconds(MAX_OBJECT_BYTES) + 60) * 1000;
const TOKEN = /^[\w-]{43}$/;
const MAX_FRAGMENT_BYTES = 128;

export interface ResumeStore {
  put: (id: string, fragment: string, now: number) => Promise<string>;
  get: (id: string, token: string, now: number) => Promise<string | undefined>;
  remove: (token: string) => Promise<void>;
}

interface ResumeRecord {
  readonly token: string;
  readonly id: string;
  readonly expiresAt: number;
  readonly key: CryptoKey;
  readonly iv: Uint8Array<ArrayBuffer>;
  readonly ciphertext: ArrayBuffer;
}

function parameters(record: Pick<ResumeRecord, 'id' | 'token' | 'expiresAt' | 'iv'>): AesGcmParams {
  return {
    name: 'AES-GCM',
    iv: record.iv,
    additionalData: utf8(`nylzo/resume/v1:${record.id}:${record.token}:${record.expiresAt}`),
    tagLength: 128,
  };
}

function isRecord(value: unknown, id: string, token: string, now: number): value is ResumeRecord {
  if (typeof value !== 'object' || value === null) {
    return false;
  }
  const record = value as Partial<ResumeRecord>;
  return (
    record.id === id &&
    record.token === token &&
    typeof record.expiresAt === 'number' &&
    Number.isSafeInteger(record.expiresAt) &&
    record.expiresAt > now &&
    record.expiresAt <= now + LIFETIME_MS &&
    record.key instanceof CryptoKey &&
    record.key.type === 'secret' &&
    record.key.algorithm.name === 'AES-GCM' &&
    (record.key.algorithm as AesKeyAlgorithm).length === 256 &&
    !record.key.extractable &&
    record.key.usages.length === 2 &&
    record.key.usages.includes('encrypt') &&
    record.key.usages.includes('decrypt') &&
    record.iv instanceof Uint8Array &&
    record.iv.length === 12 &&
    record.ciphertext instanceof ArrayBuffer &&
    record.ciphertext.byteLength >= 16 &&
    record.ciphertext.byteLength <= MAX_FRAGMENT_BYTES + 16
  );
}

function createRecords(database: IDBDatabase): void {
  const records = database.createObjectStore(RECORDS, { keyPath: 'token' });
  records.createIndex('expiresAt', 'expiresAt');
}

function deleteExpired(records: IDBObjectStore, now: number): void {
  const expired = records.index('expiresAt').openKeyCursor(IDBKeyRange.upperBound(now));
  expired.addEventListener('success', () => {
    const cursor = expired.result;
    if (!cursor) {
      return;
    }

    records.delete(cursor.primaryKey);
    cursor.continue();
  });
}

async function withRecords<Result>(
  factory: IDBFactory | undefined,
  action: (records: IDBObjectStore) => IDBRequest<Result>,
  now?: number,
): Promise<Result> {
  const database = await openDatabase(factory, DATABASE, createRecords);
  try {
    const transaction = database.transaction(RECORDS, 'readwrite');
    const records = transaction.objectStore(RECORDS);
    if (now !== undefined) {
      deleteExpired(records, now);
    }
    const request = action(records);
    await committed(transaction);
    return request.result;
  } finally {
    database.close();
  }
}

export function browserResumeStore(factory = browserDatabases()): ResumeStore {
  return {
    async put(id, fragment, now) {
      parseId(id);
      const plaintext = utf8(fragment);
      try {
        if (plaintext.length > MAX_FRAGMENT_BYTES) {
          throw new Error('invalid reading session');
        }
        const token = encodeBase64url(crypto.getRandomValues(new Uint8Array(32)));
        const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
        const fields = { id, token, expiresAt: now + LIFETIME_MS, iv: crypto.getRandomValues(new Uint8Array(12)) };
        const ciphertext = await crypto.subtle.encrypt(parameters(fields), key, plaintext);
        const record: ResumeRecord = { ...fields, key, ciphertext };
        await withRecords(factory, (records) => records.put(record), now);
        return token;
      } finally {
        plaintext.fill(0);
      }
    },
    async get(id, token, now) {
      if (!TOKEN.test(token)) {
        return;
      }
      const value: unknown = await withRecords(factory, (records) => records.get(token), now);
      if (!isRecord(value, id, token, now)) {
        await withRecords(factory, (records) => records.delete(token));
        return;
      }
      const plaintext = new Uint8Array(await crypto.subtle.decrypt(parameters(value), value.key, value.ciphertext));
      try {
        return new TextDecoder('utf-8', { fatal: true }).decode(plaintext);
      } finally {
        plaintext.fill(0);
      }
    },
    async remove(token) {
      if (TOKEN.test(token)) {
        await withRecords(factory, (records) => records.delete(token), Date.now());
      }
    },
  };
}

export function discardPlaintextRecovery(storage?: Storage): void {
  try {
    const session = storage ?? sessionStorage;
    for (let index = session.length - 1; index >= 0; index--) {
      const name = session.key(index);
      if (!name?.startsWith('nylzo:link:') || TOKEN.test(session.getItem(name) ?? '')) {
        continue;
      }

      session.removeItem(name);
      session.removeItem(name.replace('nylzo:link:', 'nylzo:lease:'));
    }
  } catch {
    return;
  }
}

export async function purgeResumeRecords(factory: IDBFactory | undefined, now: number): Promise<void> {
  try {
    if (await databaseExists(factory, DATABASE)) {
      await withRecords(factory, (records) => records.count(), now);
    }
  } catch {
    return;
  }
}
