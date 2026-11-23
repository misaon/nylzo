import { newSecret, requestKey } from '@nylzo/core';
import type { RequestKey } from '@nylzo/core';
import { IDBFactory } from 'fake-indexeddb';
import { describe, expect, it, vi } from 'vitest';

import { requestResult } from './indexed-database.ts';
import { memoryStore } from './memory-sent-store.ts';
import {
  ExtractableKeyError,
  InvalidRecordError,
  MAX_LABEL_CHARACTERS,
  recordFrom,
  RETENTION_MS,
  storedForm,
} from './sent-record.ts';
import type { SentRecord } from './sent-record.ts';
import { purgeSentRecords, rememberSent, retainedRecords } from './sent-store.ts';
import { sessionSentStore } from './session-sent-store.ts';

const NOW = Date.parse('2026-10-23T12:00:00Z');
const ID = 'AAAAAAAAAAAAAAAAAAAAAA';

async function manageKeyOf(seed: Uint8Array): Promise<RequestKey> {
  const key = await requestKey(seed);
  if (!('privateKey' in key)) {
    throw new Error('Web Crypto refused the test seed');
  }
  return key;
}

async function newRecord(): Promise<SentRecord> {
  return { id: ID, createdAt: NOW, expiresAt: NOW + 3_600_000, manageKey: await manageKeyOf(newSecret()) };
}

async function extractableKey(): Promise<RequestKey> {
  const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
  return {
    privateKey: pair.privateKey,
    publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
  };
}

describe('stored records of sent messages', () => {
  it('refuses to store an extractable manage key', async () => {
    const record = { id: ID, createdAt: NOW, expiresAt: NOW, manageKey: await extractableKey() };
    expect(() => storedForm(record)).toThrow(ExtractableKeyError);
  });

  it('stores the key object itself, never its bytes', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const stored = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW, manageKey });
    expect(stored.privateKey).toBe(manageKey.privateKey);
    expect(new Set(Object.keys(stored))).toEqual(new Set(['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey']));
  });

  it('rejects a non-extractable key with the wrong algorithm or malformed public key before writing', async () => {
    const record = await newRecord();
    const wrong = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']);
    expect(() => storedForm({ ...record, manageKey: { ...record.manageKey, privateKey: wrong.privateKey } })).toThrow(
      InvalidRecordError,
    );
    expect(() => storedForm({ ...record, manageKey: { ...record.manageKey, publicKey: new Uint8Array(31) } })).toThrow(
      InvalidRecordError,
    );
  });

  it.each([
    { id: ID, createdAt: NOW, expiresAt: NOW - 1 },
    { id: ID, createdAt: -1, expiresAt: NOW },
    { id: ID, createdAt: NOW, expiresAt: NOW + 32 * 24 * 3_600_000 },
    { id: ID, createdAt: 0.5, expiresAt: NOW },
    { id: ID, createdAt: NOW, expiresAt: 1e300 },
    { id: 'not an id', createdAt: NOW, expiresAt: NOW },
  ])('refuses to store impossible times or ids: %j', async (fields) => {
    const manageKey = await manageKeyOf(newSecret());
    expect(() => storedForm({ ...fields, manageKey })).toThrow(InvalidRecordError);
  });

  it('reads back only well-formed records with a non-extractable Ed25519 signing key', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const good = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW + 1, manageKey });
    expect(recordFrom(good)).toEqual({
      id: ID,
      createdAt: NOW,
      expiresAt: NOW + 1,
      manageKey: { privateKey: manageKey.privateKey, publicKey: manageKey.publicKey },
    });
    const loose = await extractableKey();
    const ecdsa = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify']);
    for (const malformed of [
      { ...good, privateKey: loose.privateKey },
      { ...good, privateKey: ecdsa.privateKey },
      { ...good, privateKey: 'key' },
      { ...good, publicKey: good.publicKey.slice(1) },
      { ...good, id: 'not an id' },
      { ...good, id: 42 },
      { ...good, id: `${ID.slice(0, 21)}B` },
      { ...good, createdAt: Number.NaN },
      { ...good, expiresAt: '2026-10-23' },
      { ...good, createdAt: 1e300, expiresAt: 1e300 },
      { ...good, expiresAt: NOW - 1 },
      { ...good, endedAt: NOW + 1 },
      null,
      'record',
    ]) {
      expect(recordFrom(malformed)).toBeUndefined();
    }
  });

  it('stores a record in tab memory in the same form as in IndexedDB, without an empty label', async () => {
    const store = memoryStore();
    await store.put({ ...(await newRecord()), label: '' });
    const [listed] = await store.list();
    expect(listed).toBeDefined();
    expect(listed).not.toHaveProperty('label');
    await store.put({ ...(await newRecord()), label: 'Pro Petra' });
    expect(await store.get(ID)).toMatchObject({ label: 'Pro Petra' });
  });

  it('holds records of this tab in memory until the tab closes', async () => {
    const store = sessionSentStore();
    expect(store.held()).toBe(false);
    await store.put(await newRecord());
    expect(store.held()).toBe(true);
  });

  it('keeps a short label the sender typed and drops one that is not plain text', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const labelled = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW + 1, label: 'Heslo pro Petra', manageKey });
    expect(recordFrom(labelled)?.label).toBe('Heslo pro Petra');
    expect(storedForm({ id: ID, createdAt: NOW, expiresAt: NOW + 1, label: '', manageKey })).not.toHaveProperty(
      'label',
    );
    expect(() =>
      storedForm({
        id: ID,
        createdAt: NOW,
        expiresAt: NOW + 1,
        label: 'x'.repeat(MAX_LABEL_CHARACTERS + 1),
        manageKey,
      }),
    ).toThrow(InvalidRecordError);
    for (const label of [42, '\uD800', ' padded ', 'x'.repeat(MAX_LABEL_CHARACTERS + 1)]) {
      expect(recordFrom({ ...labelled, label })).not.toHaveProperty('label');
    }
  });

  it('drops records 7 days after their message expired and sorts the rest newest first', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const store = memoryStore([
      { id: 'AAAAAAAAAAAAAAAAAAAAAA', createdAt: 1, expiresAt: NOW - RETENTION_MS, manageKey },
      { id: 'AAAAAAAAAAAAAAAAAAAAAQ', createdAt: 2, expiresAt: NOW - RETENTION_MS + 1, manageKey },
      { id: 'AAAAAAAAAAAAAAAAAAAAAg', createdAt: 3, expiresAt: NOW + 1000, manageKey },
    ]);
    const kept = await retainedRecords(store, NOW);
    expect(kept.map((record) => record.createdAt)).toEqual([3, 2]);
    expect(new Set(store.records.keys())).toEqual(new Set(['AAAAAAAAAAAAAAAAAAAAAQ', 'AAAAAAAAAAAAAAAAAAAAAg']));
  });
});

describe('cleaning up on page load', () => {
  it('neither opens nor creates the database when it does not exist', async () => {
    const factory = new IDBFactory();
    const other = await requestResult(factory.open('other'));
    other.close();
    const open = vi.spyOn(factory, 'open');
    await purgeSentRecords(factory, NOW);
    await purgeSentRecords(undefined, NOW);
    expect(open).not.toHaveBeenCalled();
    const listed = await factory.databases();
    expect(listed.map((database) => database.name)).toEqual(['other']);
  });
});

describe('remembering a new message', () => {
  it('reports a saved record', async () => {
    const store = memoryStore();
    expect(await rememberSent(store, await newRecord(), 1000)).toBe(true);
    expect(store.records.has(ID)).toBe(true);
  });

  it('reports a store that refuses the record', async () => {
    expect(await rememberSent(memoryStore([], true), await newRecord(), 1000)).toBe(false);
  });

  it('removes a record that lands after the page said it was not saved', async () => {
    const store = memoryStore();
    const gate = Promise.withResolvers<undefined>();
    const landed = Promise.withResolvers<undefined>();
    const put = store.put.bind(store);
    store.put = async (record) => {
      await gate.promise;
      await put(record);
      landed.resolve(undefined);
    };
    expect(await rememberSent(store, await newRecord(), 10)).toBe(false);
    expect(store.records.has(ID)).toBe(false);
    gate.resolve(undefined);
    await landed.promise;
    await vi.waitFor(() => expect(store.records.has(ID)).toBe(false));
  });

  it('removes nothing when a late save fails after all', async () => {
    const store = memoryStore();
    const gate = Promise.withResolvers<undefined>();
    const failed = Promise.withResolvers<undefined>();
    const remove = vi.spyOn(store, 'remove');
    store.put = async () => {
      await gate.promise;
      failed.resolve(undefined);
      throw new Error('quota exceeded');
    };
    expect(await rememberSent(store, await newRecord(), 10)).toBe(false);
    gate.resolve(undefined);
    await failed.promise;
    await Promise.resolve();
    expect(remove).not.toHaveBeenCalled();
  });
});
