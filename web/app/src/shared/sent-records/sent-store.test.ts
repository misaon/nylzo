import { type CryptoRequestKey, newSecret, requestKey } from '@nylzo/core';
import { describe, expect, it, vi } from 'vitest';

import { memoryStore } from './memory-sent-store.ts';
import {
  ExtractableKeyError,
  InvalidRecordError,
  recordFrom,
  RETENTION_MS,
  type SentRecord,
  storedForm,
} from './sent-record.ts';
import { purgeSentRecords, rememberSent, retainedRecords } from './sent-store.ts';

const NOW = Date.parse('2026-10-23T12:00:00Z');
const ID = 'AAAAAAAAAAAAAAAAAAAAAA';

async function manageKeyOf(seed: Uint8Array): Promise<CryptoRequestKey> {
  const key = await requestKey(seed);
  if (!('privateKey' in key)) {
    throw new Error('Web Crypto refused the test seed');
  }
  return key;
}

async function extractableKey(): Promise<CryptoRequestKey> {
  const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
  return {
    privateKey: pair.privateKey,
    publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
  };
}

describe('stored records of sent messages (D39)', () => {
  it('refuses to store an extractable manage key', async () => {
    const record = { id: ID, createdAt: NOW, expiresAt: NOW, manageKey: await extractableKey() };
    expect(() => storedForm(record)).toThrow(ExtractableKeyError);
  });

  it('stores the key object itself, never its bytes', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const stored = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW, manageKey });
    expect(stored.privateKey).toBe(manageKey.privateKey);
    expect(Object.keys(stored).sort()).toEqual(['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey']);
  });

  it('refuses to store impossible times', async () => {
    const manageKey = await manageKeyOf(newSecret());
    for (const times of [
      { createdAt: NOW, expiresAt: NOW - 1 },
      { createdAt: -1, expiresAt: NOW },
      { createdAt: NOW, expiresAt: NOW + 32 * 24 * 3_600_000 },
      { createdAt: 0.5, expiresAt: NOW },
      { createdAt: NOW, expiresAt: 1e300 },
    ]) {
      expect(() => storedForm({ id: ID, ...times, manageKey }), JSON.stringify(times)).toThrow(InvalidRecordError);
    }
    expect(() => storedForm({ id: 'not an id', createdAt: NOW, expiresAt: NOW, manageKey })).toThrow(
      InvalidRecordError,
    );
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
    for (const bad of [
      { ...good, privateKey: loose.privateKey },
      { ...good, privateKey: ecdsa.privateKey },
      { ...good, privateKey: 'key' },
      { ...good, publicKey: good.publicKey.slice(1) },
      { ...good, id: 'not an id' },
      { ...good, id: `${ID.slice(0, 21)}B` },
      { ...good, createdAt: NaN },
      { ...good, expiresAt: '2026-10-23' },
      { ...good, createdAt: 1e300, expiresAt: 1e300 },
      { ...good, expiresAt: NOW - 1 },
      { ...good, endedAt: -5 },
      null,
      'record',
    ]) {
      expect(recordFrom(bad)).toBeUndefined();
    }
  });

  it('drops records 7 days after their message expired and sorts the rest newest first', async () => {
    const manageKey = await manageKeyOf(newSecret());
    const make = (id: string, createdAt: number, expiresAt: number) => ({ id, createdAt, expiresAt, manageKey });
    const store = memoryStore([
      make('AAAAAAAAAAAAAAAAAAAAAA', 1, NOW - RETENTION_MS),
      make('AAAAAAAAAAAAAAAAAAAAAQ', 2, NOW - RETENTION_MS + 1),
      make('AAAAAAAAAAAAAAAAAAAAAg', 3, NOW + 1000),
    ]);
    const kept = await retainedRecords(store, NOW);
    expect(kept.map((record) => record.createdAt)).toEqual([3, 2]);
    expect([...store.records.keys()].sort()).toEqual(['AAAAAAAAAAAAAAAAAAAAAQ', 'AAAAAAAAAAAAAAAAAAAAAg']);
  });
});

describe('cleaning up on page load', () => {
  it('neither opens nor creates the database when it does not exist', async () => {
    const opened: string[] = [];
    const factory = {
      databases: async () => [{ name: 'other', version: 1 }],
      open: (name: string) => {
        opened.push(name);
        throw new Error('must not open');
      },
    } as unknown as IDBFactory;
    await purgeSentRecords(factory, NOW);
    await purgeSentRecords({ open: factory.open } as unknown as IDBFactory, NOW);
    await purgeSentRecords(undefined, NOW);
    expect(opened).toEqual([]);
  });
});

describe('remembering a new message', () => {
  async function record(): Promise<SentRecord> {
    return { id: ID, createdAt: NOW, expiresAt: NOW + 3_600_000, manageKey: await manageKeyOf(newSecret()) };
  }

  it('reports a saved record', async () => {
    const store = memoryStore();
    expect(await rememberSent(store, await record(), 1000)).toBe(true);
    expect(store.records.has(ID)).toBe(true);
  });

  it('reports a store that refuses the record', async () => {
    expect(await rememberSent(memoryStore([], true), await record(), 1000)).toBe(false);
  });

  it('removes a record that lands after the page said it was not saved', async () => {
    const store = memoryStore();
    const gate = Promise.withResolvers<void>();
    const landed = Promise.withResolvers<void>();
    const put = store.put.bind(store);
    store.put = async (value) => {
      await gate.promise;
      await put(value);
      landed.resolve();
    };
    expect(await rememberSent(store, await record(), 10)).toBe(false);
    expect(store.records.has(ID)).toBe(false);
    gate.resolve();
    await landed.promise;
    await vi.waitFor(() => expect(store.records.has(ID)).toBe(false));
  });
});
