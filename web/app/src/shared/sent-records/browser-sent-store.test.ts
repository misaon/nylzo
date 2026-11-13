import { requestKey } from '@nylzo/core';
import { IDBFactory } from 'fake-indexeddb';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { committed, openDatabase, requestResult } from './indexed-database.ts';
import { RETENTION_MS } from './sent-record.ts';
import type { SentRecord } from './sent-record.ts';
import { browserSentStore, keepPurgingSentRecords, purgeSentRecords } from './sent-store.ts';
import type { SentStore } from './sent-store.ts';

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

  it('reads, forgets and clears without creating a database nobody asked for', async () => {
    expect(await store.list()).toEqual([]);
    await store.remove(record.id);
    await store.clear();
    expect(await factory.databases()).toEqual([]);
  });

  it('creates no database when the browser cannot list its databases in time, and opens one when listing fails', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const hanging = browserSentStore(
        Object.assign(Object.create(factory) as IDBFactory, {
          databases: async () => Promise.withResolvers<IDBDatabaseInfo[]>().promise,
        }),
      );
      const listed = hanging.list();
      await vi.advanceTimersByTimeAsync(2000);
      expect(await listed).toEqual([]);
      expect(await factory.databases()).toEqual([]);
    } finally {
      vi.useRealTimers();
    }
    const failing = browserSentStore(
      Object.assign(Object.create(factory) as IDBFactory, { databases: () => Promise.reject(new Error('refused')) }),
    );
    expect(await failing.list()).toEqual([]);
    const created = await factory.databases();
    expect(created.map((database) => database.name)).toEqual(['nylzo']);
  });

  it('lets another tab delete the database while it is open, then opens it again', async () => {
    await store.put(record);
    const deletion = factory.deleteDatabase('nylzo');
    const outcome = await new Promise<string>((resolve) => {
      deletion.addEventListener('success', () => resolve('deleted'));
      deletion.addEventListener('blocked', () => resolve('blocked'));
    });
    expect(outcome).toBe('deleted');
    await store.put(record);
    expect(await store.list()).toHaveLength(1);
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

  it.each([
    ['a malformed key', { publicKey: 'invalid' }],
    ['the end an older client marked', { endedAt: NOW }],
  ])('removes a stored record with %s instead of listing it', async (_case, change) => {
    await store.put(record);
    const database = await openDatabase(factory, 'nylzo', () => {
      throw new Error('must not recreate stores');
    });
    const transaction = database.transaction('sent', 'readwrite');
    const done = committed(transaction);
    const records = transaction.objectStore('sent');
    const stored: unknown = await requestResult(records.get(record.id));
    await requestResult(records.put(Object.assign({}, stored, change)));
    await done;
    await expect(store.list()).resolves.toEqual([]);
    const read = database.transaction('sent', 'readonly');
    await expect(requestResult(read.objectStore('sent').get(record.id))).resolves.toBeUndefined();
    database.close();
  });

  it('checks again every hour and when a record is due, so an open page removes a record saved after it started', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      let clock = NOW;
      await keepPurgingSentRecords(factory, () => clock);
      expect(vi.getTimerCount()).toBe(1);
      await store.put(record);
      clock += 3_600_000;
      await vi.advanceTimersByTimeAsync(3_600_000);
      await vi.waitFor(async () => expect(await factory.databases()).toHaveLength(1));
      clock = record.expiresAt + RETENTION_MS;
      await vi.advanceTimersByTimeAsync(record.expiresAt + RETENTION_MS - NOW);
      await vi.waitFor(async () => expect(await store.list()).toEqual([]));
      expect(vi.getTimerCount()).toBe(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('keeps checking every hour when the browser never answers the database listing', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const hanging = Object.assign(Object.create(factory) as IDBFactory, {
        databases: async () => Promise.withResolvers<IDBDatabaseInfo[]>().promise,
      });
      const purging = keepPurgingSentRecords(hanging, () => NOW);
      await vi.advanceTimersByTimeAsync(2000);
      await purging;
      expect(vi.getTimerCount()).toBe(1);
      expect(await factory.databases()).toEqual([]);
    } finally {
      vi.useRealTimers();
    }
  });

  it('purges expired records, names the next purge and creates no database for a browser with no records', async () => {
    await expect(purgeSentRecords(factory, NOW)).resolves.toBeUndefined();
    await expect(factory.databases()).resolves.toEqual([]);
    await store.put(record);
    await store.put({ ...record, id: 'AQEBAQEBAQEBAQEBAQEBAQ', expiresAt: record.expiresAt + 1000 });
    await expect(purgeSentRecords(factory, NOW)).resolves.toBe(record.expiresAt + RETENTION_MS);
    await expect(purgeSentRecords(factory, record.expiresAt + RETENTION_MS)).resolves.toBe(
      record.expiresAt + 1000 + RETENTION_MS,
    );
    await expect(store.list()).resolves.toMatchObject([{ id: 'AQEBAQEBAQEBAQEBAQEBAQ' }]);
  });
});
