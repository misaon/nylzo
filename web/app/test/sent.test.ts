import { newSecret, requestKey, type RequestKey } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../src/lib/api.ts';
import { createMessage, type CreateDeps } from '../src/lib/create.ts';
import {
  ExtractableKeyError,
  RETENTION_MS,
  createSentPage,
  isCancellable,
  recordFrom,
  retainedRecords,
  storedForm,
  type SentRecord,
  type SentStore,
  type SentView,
} from '../src/lib/sent.ts';
import { fakeApi, type FakeApi, type FakeOptions } from './fake-api.ts';

const NOW = Date.parse('2026-10-23T12:00:00Z');
const ID = 'AAAAAAAAAAAAAAAAAAAAAA';

function memoryStore(initial: SentRecord[] = [], broken = false): SentStore & { readonly records: Map<string, SentRecord> } {
  const records = new Map(initial.map((record) => [record.id, record]));
  const check = () => {
    if (broken) {
      throw new Error('storage disabled');
    }
  };
  return {
    records,
    async list() {
      check();
      return [...records.values()];
    },
    async put(record) {
      check();
      storedForm(record);
      records.set(record.id, record);
    },
    async remove(id) {
      check();
      records.delete(id);
    },
    async clear() {
      check();
      records.clear();
    },
  };
}

async function remembered(api: FakeApi, createdAt: number, opensMax = 1): Promise<SentRecord> {
  const deps: CreateDeps = { api, origin: 'https://nylzo.test', scrypt: async () => new Uint8Array(32), solvePow: async () => '' };
  const created = await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax, passphrase: '', kdfId: 0 }, deps);
  return { id: created.id, createdAt, expiresAt: NOW + 3_600_000, manageKey: created.manageKey };
}

function page(api: FakeApi, store: SentStore) {
  const views: SentView[] = [];
  const sent = createSentPage({ api, store, now: () => NOW }, (view) => {
    views.push(view);
  });
  const items = () => {
    const last = views.at(-1);
    return last?.view === 'list' ? last.items : [];
  };
  return { ...sent, views, last: () => views.at(-1), items };
}

async function setup(options: FakeOptions = {}, count = 2) {
  const api = fakeApi(options);
  const records: SentRecord[] = [];
  for (let index = 0; index < count; index++) {
    records.push(await remembered(api, NOW - (count - index) * 1000, 2));
  }
  const store = memoryStore(records);
  return { api, records, store, sent: page(api, store) };
}

async function extractableKey(): Promise<RequestKey> {
  const pair = (await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify'])) as CryptoKeyPair;
  return { privateKey: pair.privateKey, publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)) };
}

describe('stored records of sent messages (D39)', () => {
  it('refuses to store an extractable manage key', async () => {
    const record = { id: ID, createdAt: NOW, expiresAt: NOW, manageKey: await extractableKey() };
    expect(() => storedForm(record)).toThrow(ExtractableKeyError);
  });

  it('stores the key object itself, never its bytes', async () => {
    const manageKey = await requestKey(newSecret());
    const stored = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW, manageKey });
    expect(stored.privateKey).toBe(manageKey.privateKey);
    expect(Object.keys(stored).sort()).toEqual(['createdAt', 'expiresAt', 'id', 'privateKey', 'publicKey']);
  });

  it('reads back only well-formed records with a non-extractable Ed25519 signing key', async () => {
    const manageKey = await requestKey(newSecret());
    const good = storedForm({ id: ID, createdAt: NOW, expiresAt: NOW + 1, manageKey });
    expect(recordFrom(good)).toEqual({ id: ID, createdAt: NOW, expiresAt: NOW + 1, manageKey: { privateKey: manageKey.privateKey, publicKey: manageKey.publicKey } });
    const loose = await extractableKey();
    const ecdsa = (await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, false, ['sign', 'verify'])) as CryptoKeyPair;
    for (const bad of [
      { ...good, privateKey: loose.privateKey },
      { ...good, privateKey: ecdsa.privateKey },
      { ...good, privateKey: 'key' },
      { ...good, publicKey: good.publicKey.slice(1) },
      { ...good, id: 'not an id' },
      { ...good, id: `${ID.slice(0, 21)}B` },
      { ...good, createdAt: Number.NaN },
      { ...good, expiresAt: '2026-10-23' },
      null,
      'record',
    ]) {
      expect(recordFrom(bad)).toBeUndefined();
    }
  });

  it('drops records 7 days after their message expired and sorts the rest newest first', async () => {
    const manageKey = await requestKey(newSecret());
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

describe('the sent page', () => {
  it('loads the status of every message one after another, newest first', async () => {
    const { api, records, sent } = await setup();
    api.calls.length = 0;
    await sent.load();
    expect(api.calls).toEqual(['status', 'status']);
    expect(sent.views[0]).toEqual({ view: 'loading' });
    expect(sent.items().map((item) => item.id)).toEqual([records[1]?.id, records[0]?.id]);
    expect(sent.items().map((item) => item.state)).toEqual([
      { kind: 'loaded', status: expect.objectContaining({ state: 'available', opensUsed: 0, opensMax: 2 }) },
      { kind: 'loaded', status: expect.objectContaining({ state: 'available' }) },
    ]);
  });

  it('stops at a rate limit and marks the rest to try later', async () => {
    const { api, sent } = await setup({ failStatus: [new ApiError(429, 'rate_limited', 20)] }, 3);
    api.calls.length = 0;
    await sent.load();
    expect(api.calls).toEqual(['status']);
    expect(sent.items().map((item) => item.state)).toEqual(Array(3).fill({ kind: 'retry_later', retryAfter: 20 }));
  });

  it('shows a message the server no longer knows and a failed status separately', async () => {
    const { sent } = await setup({ failStatus: [new ApiError(404, 'not_found'), new ApiError(500, 'internal')] });
    await sent.load();
    expect(sent.items().map((item) => item.state.kind)).toEqual(['unknown', 'failed']);
  });

  it('cancels a live message and shows its new state', async () => {
    const { api, records, sent } = await setup({}, 1);
    await sent.load();
    const id = records[0]!.id;
    expect(isCancellable(sent.items()[0]!.state)).toBe(true);
    expect(await sent.cancel(id)).toEqual({ outcome: 'cancelled' });
    const state = sent.items()[0]!.state;
    expect(state).toMatchObject({ kind: 'loaded', status: { state: 'cancelled', endedAt: '2026-10-23T11:00:00Z' } });
    expect(isCancellable(state)).toBe(false);
    expect(api.stored.get(id)?.state).toBe('cancelled');
    expect(await sent.cancel(id)).toEqual({ outcome: 'ended' });
  });

  it('reports a rate limited cancel and leaves the message as it was', async () => {
    const { records, sent } = await setup({ failCancel: [new ApiError(503, 'unavailable', 5)] }, 1);
    await sent.load();
    expect(await sent.cancel(records[0]!.id)).toEqual({ outcome: 'retry_later', retryAfter: 5 });
    expect(sent.items()[0]?.state).toMatchObject({ kind: 'loaded', status: { state: 'available' } });
  });

  it('ignores a cancel for a message it does not list', async () => {
    const { api, sent } = await setup({}, 1);
    await sent.load();
    api.calls.length = 0;
    expect(await sent.cancel(ID)).toEqual({ outcome: 'ignored' });
    expect(api.calls).toEqual([]);
  });

  it('forgets one message or all of them in the store too', async () => {
    const { records, store, sent } = await setup({}, 3);
    await sent.load();
    await sent.forget(records[0]!.id);
    expect(sent.items()).toHaveLength(2);
    expect(store.records.has(records[0]!.id)).toBe(false);
    await sent.forgetAll();
    expect(sent.last()).toEqual({ view: 'list', items: [] });
    expect(store.records.size).toBe(0);
  });

  it('says so when the browser does not let it store anything', async () => {
    const sent = page(fakeApi(), memoryStore([], true));
    await sent.load();
    expect(sent.last()).toEqual({ view: 'unavailable' });
  });

  it('treats only a live message without an end as cancellable', () => {
    const status = { state: 'available' as const, opensUsed: 0, opensMax: 1, failedPassphraseAttempts: 0 };
    expect(isCancellable({ kind: 'loaded', status })).toBe(true);
    expect(isCancellable({ kind: 'loaded', status: { ...status, state: 'opened' } })).toBe(true);
    expect(isCancellable({ kind: 'loaded', status: { ...status, state: 'opened', endedAt: '2026-10-23T11:00:00Z' } })).toBe(false);
    expect(isCancellable({ kind: 'loaded', status: { ...status, state: 'expired' } })).toBe(false);
    expect(isCancellable({ kind: 'loaded', status: { ...status, state: 'cancelled' } })).toBe(false);
    expect(isCancellable({ kind: 'unknown' })).toBe(false);
  });
});
