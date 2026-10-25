import { type CryptoRequestKey, newSecret, requestKey } from '@nylzo/core';
import { describe, expect, it, vi } from 'vitest';

import { newGate } from '../testing/gate.ts';
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

async function newRecord(): Promise<SentRecord> {
  return { id: ID, createdAt: NOW, expiresAt: NOW + 3_600_000, manageKey: await manageKeyOf(newSecret()) };
}

function openNothing(name: string): never {
  throw new Error(`must not open ${name}`);
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
    expect(new Set(Object.keys(stored))).toEqual(new Set(['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey']));
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
      { ...good, id: `${ID.slice(0, 21)}B` },
      { ...good, createdAt: NaN },
      { ...good, expiresAt: '2026-10-23' },
      { ...good, createdAt: 1e300, expiresAt: 1e300 },
      { ...good, expiresAt: NOW - 1 },
      { ...good, endedAt: -5 },
      null,
      'record',
    ]) {
      expect(recordFrom(malformed)).toBeUndefined();
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
    expect(new Set(store.records.keys())).toEqual(new Set(['AAAAAAAAAAAAAAAAAAAAAQ', 'AAAAAAAAAAAAAAAAAAAAAg']));
  });
});

describe('cleaning up on page load', () => {
  it('neither opens nor creates the database when it does not exist', async () => {
    const open = vi.fn(openNothing);
    const factory = { databases: () => Promise.resolve([{ name: 'other', version: 1 }]), open };
    await purgeSentRecords(factory as unknown as IDBFactory, NOW);
    await purgeSentRecords({ open } as unknown as IDBFactory, NOW);
    await purgeSentRecords(undefined, NOW);
    expect(open).not.toHaveBeenCalled();
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
    const gate = newGate();
    const landed = newGate();
    const put = store.put.bind(store);
    store.put = async (record) => {
      await gate.opened;
      await put(record);
      landed.open();
    };
    expect(await rememberSent(store, await newRecord(), 10)).toBe(false);
    expect(store.records.has(ID)).toBe(false);
    gate.open();
    await landed.opened;
    await vi.waitFor(() => expect(store.records.has(ID)).toBe(false));
  });
});
