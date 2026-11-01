import { requestKey } from '@nylzo/core';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it } from 'vitest';

import { committed, openDatabase, requestResult } from './indexed-database.ts';
import { RETENTION_MS, type SentRecord } from './sent-record.ts';
import { browserSentStore, purgeSentRecords, type SentStore } from './sent-store.ts';

const NOW = Date.parse('2026-11-01T12:00:00Z');

describe('persistent sent records', () => {
  let factory: IDBFactory;
  let store: SentStore;
  let record: SentRecord;

  beforeEach(async () => {
    factory = new IDBFactory();
    store = browserSentStore(factory);
    record = {
      id: 'AAAAAAAAAAAAAAAAAAAAAA',
      createdAt: NOW,
      expiresAt: NOW + 3_600_000,
      manageKey: await requestKey(new Uint8Array(32).fill(7)),
    };
  });

  it('persists a non-extractable signing key and reads it from a new store instance', async () => {
    await store.put(record);
    const restored = await browserSentStore(factory).list();
    expect(restored).toHaveLength(1);
    expect(restored[0]).toMatchObject({ id: record.id, createdAt: NOW, expiresAt: record.expiresAt });
    const key = restored[0]?.manageKey.privateKey;
    expect(key?.extractable).toBe(false);
    if (!key) {
      throw new Error('missing restored signing key');
    }
    const payload = new Uint8Array([1, 2, 3]);
    const signature = await crypto.subtle.sign('Ed25519', key, payload);
    const publicKey = await crypto.subtle.importKey('raw', record.manageKey.publicKey, 'Ed25519', false, ['verify']);
    await expect(crypto.subtle.verify('Ed25519', publicKey, signature, payload)).resolves.toBe(true);
  });

  it('marks a record ended once, preserves that timestamp and ignores absent records', async () => {
    await store.put(record);
    await expect(store.markEnded('missing', NOW)).resolves.toBeUndefined();
    await expect(store.markEnded(record.id, NOW + 1000)).resolves.toMatchObject({ endedAt: NOW + 1000 });
    await expect(store.markEnded(record.id, NOW + 2000)).resolves.toMatchObject({ endedAt: NOW + 1000 });
    expect(await store.list()).toMatchObject([{ endedAt: NOW + 1000 }]);
  });

  it('removes individual records and clears the database only when requested', async () => {
    const second = { ...record, id: 'AQEBAQEBAQEBAQEBAQEBAQ' };
    await store.put(record);
    await store.put(second);
    await store.remove(record.id);
    const remaining = await store.list();
    expect(remaining.map((value) => value.id)).toEqual([second.id]);
    await store.clear();
    await expect(store.list()).resolves.toEqual([]);
  });

  it('removes corrupt stored records instead of exposing malformed keys', async () => {
    await store.put(record);
    const database = await openDatabase(factory, 'nylzo', () => {
      throw new Error('must not recreate stores');
    });
    const transaction = database.transaction('sent', 'readwrite');
    const done = committed(transaction);
    await requestResult(transaction.objectStore('sent').put({ id: record.id, publicKey: 'invalid' }));
    await done;
    await expect(store.list()).resolves.toEqual([]);
    const read = database.transaction('sent', 'readonly');
    await expect(requestResult(read.objectStore('sent').get(record.id))).resolves.toBeUndefined();
    database.close();
  });

  it('purges expired records without creating a database for a browser with no records', async () => {
    await purgeSentRecords(factory, NOW);
    await expect(factory.databases()).resolves.toEqual([]);
    await store.put(record);
    await purgeSentRecords(factory, record.expiresAt + RETENTION_MS + 1);
    await expect(store.list()).resolves.toEqual([]);
  });
});
