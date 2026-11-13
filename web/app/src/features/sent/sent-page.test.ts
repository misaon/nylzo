import { idToString, KDF_NONE, newRequestKey, newShareKey, parseId } from '@nylzo/core';
import { describe, expect, it, vi } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import { fakeApi } from '../../shared/api/fake-api.ts';
import type { FakeApi, FakeOptions } from '../../shared/api/fake-api.ts';
import { memoryStore } from '../../shared/sent-records/memory-sent-store.ts';
import { isRetained, RETENTION_MS } from '../../shared/sent-records/sent-record.ts';
import type { SentRecord } from '../../shared/sent-records/sent-record.ts';
import type { SentStore } from '../../shared/sent-records/sent-store.ts';
import { assess, createSentPage } from './sent-page.ts';
import type { SentPage, SentView } from './sent-page.ts';

const NOW = Date.parse('2026-10-23T12:00:00Z');

async function remembered(api: FakeApi, createdAt: number, opensMax = 1): Promise<SentRecord> {
  const { create } = await api.instance({ ttlSeconds: 3600, shareKey: newShareKey().publicKey });
  if (create === undefined) {
    throw new Error('the fake server issued no id');
  }
  const manageKey = await newRequestKey();
  await api.create({
    id: create.id,
    idToken: create.idToken,
    epoch: create.epoch,
    deadline: create.deadline,
    readKey: new Uint8Array(32),
    verifierKey: new Uint8Array(32).fill(1),
    manageKey: manageKey.publicKey,
    kdfId: KDF_NONE,
    opensMax,
    ttlSeconds: 3600,
    object: new Uint8Array(0),
  });
  return { id: idToString(create.id), createdAt, expiresAt: NOW + 3_600_000, manageKey };
}

function page(api: FakeApi, browser: SentStore | undefined, session: SentStore = memoryStore()) {
  const views: SentView[] = [];
  const sent = createSentPage({ api, session, browser, now: () => NOW }, (view) => {
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
  const record = (index: number): SentRecord => records[index] ?? missing(`no record ${index}`);
  return { api, records, record, store, sent: page(api, store) };
}

describe('the sent page', () => {
  it('loads the status of every message one after another, newest first', async () => {
    const { api, records, sent } = await setup();
    api.calls.length = 0;
    await sent.load();
    expect(api.calls).toEqual(['status', 'status']);
    expect(sent.views[0]).toEqual({ view: 'loading' });
    expect(sent.items().map((item) => item.id)).toEqual([records[1]?.id, records[0]?.id]);
    expect(sent.items().map((item) => item.state)).toMatchObject([
      { kind: 'loaded', status: { state: 'available', opensUsed: 0, opensMax: 2 } },
      { kind: 'loaded', status: { state: 'available' } },
    ]);
  });

  it('stops at a rate limit and marks the rest to try later', async () => {
    const { api, sent } = await setup({ failStatus: [new ApiError(429, 'rate_limited', { retryAfter: 20 })] }, 3);
    api.calls.length = 0;
    await sent.load();
    expect(api.calls).toEqual(['status']);
    expect(sent.items().map((item) => item.state)).toEqual(
      Array.from({ length: 3 }, () => ({ kind: 'retry_later', retryAfter: 20 })),
    );
  });

  it('shows a message the server no longer knows and a failed status separately, and keeps both stored', async () => {
    const { store, sent } = await setup({
      failStatus: [new ApiError(404, 'not_found'), new ApiError(500, 'internal')],
    });
    await sent.load();
    expect(sent.items().map((item) => item.state.kind)).toEqual(['unknown', 'failed']);
    expect(store.records.size).toBe(2);
  });

  it('keeps the key of a message the server still reports live, even when the local clock says it expired', async () => {
    const { api, record, store } = await setup({}, 1);
    const sent = page(api, store);
    const status = api.status.bind(api);
    api.status = async (id, key) => ({ ...(await status(id, key)), expiresAt: new Date(NOW - 1000).toISOString() });
    await sent.load();
    expect(sent.items()[0]).toMatchObject({
      state: { status: { state: 'available' } },
      ended: false,
      cancellable: true,
    });
    expect(store.records.has(record(0).id)).toBe(true);
  });

  it('removes a cancelled message from storage even when its final status cannot load', async () => {
    const { api, record, store, sent } = await setup({}, 1);
    await sent.load();
    api.status = vi.fn<FakeApi['status']>().mockRejectedValue(new ApiError(503, 'unavailable', { retryAfter: 5 }));
    expect(await sent.cancel(record(0).id)).toEqual({ outcome: 'cancelled' });
    expect(store.records.size).toBe(0);
    expect(sent.items()[0]).toMatchObject({
      state: { kind: 'retry_later', retryAfter: 5 },
      ended: true,
      cancellable: false,
    });
  });

  it.each([
    { name: 'ended elsewhere', options: {}, endFirst: true, outcome: 'ended', kept: 0 },
    {
      name: 'unknown to the server',
      options: { failCancel: [new ApiError(404, 'not_found')] },
      endFirst: false,
      outcome: 'unknown',
      kept: 1,
    },
  ] satisfies { name: string; options: FakeOptions; endFirst: boolean; outcome: string; kept: number }[])(
    'handles a cancel of a message $name',
    async ({ options, endFirst, outcome, kept }) => {
      const { api, record, store, sent } = await setup(options, 1);
      await sent.load();
      if (endFirst) {
        await api.cancel(parseId(record(0).id), record(0).manageKey);
      }
      expect(await sent.cancel(record(0).id)).toEqual({ outcome });
      expect(store.records.size).toBe(kept);
    },
  );

  it('removes a cancelled message from the store it came from even when the page stops before the answer', async () => {
    const { api, record, store, sent } = await setup({}, 1);
    await sent.load();
    const answer = Promise.withResolvers<undefined>();
    const cancel = api.cancel.bind(api);
    api.cancel = async (id, key) => {
      await answer.promise;
      await cancel(id, key);
    };
    const cancelling = sent.cancel(record(0).id);
    await Promise.resolve();
    sent.stop();
    answer.resolve(undefined);
    expect(await cancelling).toEqual({ outcome: 'cancelled' });
    expect(store.records.size).toBe(0);
  });

  it('says so when storage refuses to remove an ended message or to list the browser records', async () => {
    const { api, record, store, sent } = await setup({}, 1);
    await api.cancel(parseId(record(0).id), record(0).manageKey);
    store.remove = vi.fn<typeof store.remove>().mockRejectedValue(new Error('storage denied'));
    await sent.load();
    expect(sent.last()).toMatchObject({ view: 'list', storageFailed: true });
    const tracked = page(api, memoryStore([], true), memoryStore([await remembered(api, NOW)]));
    await tracked.load();
    expect(tracked.last()).toMatchObject({ view: 'list', storageFailed: true });
  });

  it('stops a running load and clears what it showed', async () => {
    const { api, sent } = await setup({}, 3);
    api.calls.length = 0;
    const loading = sent.load();
    sent.stop();
    await loading;
    expect(sent.last()).toEqual({ view: 'loading' });
    expect(api.calls.filter((call) => call === 'status').length).toBeLessThan(3);
  });

  it('cancels a live message and shows its new state', async () => {
    const { api, record, sent } = await setup({}, 1);
    await sent.load();
    const { id } = record(0);
    expect(sent.items()[0]).toMatchObject({ ended: false, cancellable: true });
    expect(await sent.cancel(id)).toEqual({ outcome: 'cancelled' });
    expect(sent.items()[0]).toMatchObject({
      state: { kind: 'loaded', status: { state: 'cancelled', endedAt: '2026-10-23T11:00:00Z', opensMax: 0 } },
      ended: true,
      cancellable: false,
    });
    expect(api.stored.get(id)?.state).toBe('cancelled');
    expect(await sent.cancel(id)).toEqual({ outcome: 'ignored' });
  });

  it('cancels a message once even when asked twice at the same time', async () => {
    const { api, record, sent } = await setup({}, 1);
    await sent.load();
    api.calls.length = 0;
    const { id } = record(0);
    const [first, second] = await Promise.all([sent.cancel(id), sent.cancel(id)]);
    expect([first, second]).toEqual([{ outcome: 'cancelled' }, { outcome: 'ignored' }]);
    expect(api.calls.filter((call) => call === 'cancel')).toHaveLength(1);
  });

  it('reports a rate limited cancel and leaves the message as it was', async () => {
    const { record, sent } = await setup({ failCancel: [new ApiError(503, 'unavailable', { retryAfter: 5 })] }, 1);
    await sent.load();
    expect(await sent.cancel(record(0).id)).toEqual({ outcome: 'retry_later', retryAfter: 5 });
    expect(sent.items()[0]?.state).toMatchObject({ kind: 'loaded', status: { state: 'available' } });
  });

  it('forgets one message or all of them in the store too', async () => {
    const { record, store, sent } = await setup({}, 3);
    await sent.load();
    await sent.forget(record(0).id);
    expect(sent.items()).toHaveLength(2);
    expect(store.records.has(record(0).id)).toBe(false);
    await sent.forgetAll();
    expect(sent.last()).toEqual({ view: 'list', items: [], storageFailed: false });
    expect(store.records.size).toBe(0);
  });

  it('says so when the browser does not let it store anything, unless this tab tracks a message', async () => {
    const api = fakeApi();
    const sent = page(api, memoryStore([], true));
    await sent.load();
    expect(sent.last()).toEqual({ view: 'unavailable' });
    const tracked = page(api, memoryStore([], true), memoryStore([await remembered(api, NOW)]));
    await tracked.load();
    expect(tracked.items()).toMatchObject([{ session: true, state: { kind: 'loaded' } }]);
  });

  it('lists messages of this tab and of the browser together and acts on the store that holds each', async () => {
    const api = fakeApi();
    const kept = await remembered(api, NOW - 2000);
    const tracked = await remembered(api, NOW - 1000);
    const browser = memoryStore([kept]);
    const session = memoryStore([tracked]);
    const sent = page(api, browser, session);
    await sent.load();
    expect(sent.items().map((item) => [item.id, item.session])).toEqual([
      [tracked.id, true],
      [kept.id, false],
    ]);
    expect(await sent.cancel(tracked.id)).toEqual({ outcome: 'cancelled' });
    expect(session.records.size).toBe(0);
    expect(browser.records.has(kept.id)).toBe(true);
    await sent.forget(kept.id);
    expect(browser.records.size).toBe(0);
    session.records.set(tracked.id, tracked);
    browser.records.set(kept.id, kept);
    await sent.load();
    await sent.forgetAll();
    expect([session.records.size, browser.records.size]).toEqual([0, 0]);
    expect(sent.items()).toEqual([]);
  });

  it('treats a message the server reports live, or one whose status did not load before expiry, as cancellable', () => {
    const withoutExpiry = { state: 'available' as const, opensUsed: 0, opensMax: 1, failedPassphraseAttempts: 0 };
    const status = { ...withoutExpiry, expiresAt: '2026-10-23T13:00:00Z' };
    const later = NOW + 3_600_000;
    expect(assess({ kind: 'loaded', status }, later, NOW)).toEqual({ ended: false, cancellable: true });
    expect(assess({ kind: 'loaded', status: { ...status, state: 'opened' } }, later, NOW)).toEqual({
      ended: false,
      cancellable: true,
    });
    const endedStatuses = [
      { ...status, state: 'opened' as const, endedAt: '2026-10-23T11:00:00Z' },
      { ...status, state: 'expired' as const },
      { ...status, state: 'cancelled' as const },
      { ...status, state: 'deleted' as const },
    ];
    expect(endedStatuses.map((ended) => assess({ kind: 'loaded', status: ended }, later, NOW))).toEqual(
      endedStatuses.map(() => ({ ended: true, cancellable: false })),
    );
    expect(assess({ kind: 'loaded', status: { ...status, expiresAt: '2026-10-23T11:00:00Z' } }, NOW, NOW)).toEqual({
      ended: false,
      cancellable: true,
    });
    expect(assess({ kind: 'unknown' }, later, NOW)).toEqual({ ended: false, cancellable: false });
    expect(assess({ kind: 'failed' }, later, NOW)).toEqual({ ended: false, cancellable: true });
    expect(assess({ kind: 'retry_later', retryAfter: 30 }, later, NOW)).toEqual({ ended: false, cancellable: true });
    expect(assess({ kind: 'failed' }, NOW, NOW)).toEqual({ ended: false, cancellable: false });
  });

  it('never signs with a key that another tab forgot', async () => {
    const { api, record, store, sent } = await setup({}, 2);
    await sent.load();
    const { id } = record(0);
    store.records.delete(id);
    api.calls.length = 0;
    expect(await sent.cancel(id)).toEqual({ outcome: 'ignored' });
    await sent.refresh(id);
    expect(api.calls).toEqual([]);
    expect(sent.items().map((item) => item.id)).toEqual([record(1).id]);
  });

  it('skips a message forgotten while the list loads and still marks the rest to try later', async () => {
    const { api, record, store, sent } = await setup(
      { failStatus: [new ApiError(500, 'internal'), new ApiError(429, 'rate_limited', { retryAfter: 9 })] },
      4,
    );
    api.calls.length = 0;
    const { id } = record(2);
    const loading = sent.load();
    await sent.forget(id);
    await loading;
    expect(store.records.has(id)).toBe(false);
    expect(api.calls.filter((call) => call === 'status')).toHaveLength(2);
    expect(sent.items().map((item) => item.state.kind)).toEqual(['failed', 'retry_later', 'retry_later']);
  });

  it('removes an ended message from storage once it shows the outcome, and keeps an unseen one a day past expiry (F16)', async () => {
    const { api, record, store, sent } = await setup({}, 2);
    await api.cancel(parseId(record(0).id), record(0).manageKey);
    await sent.load();
    expect(sent.items().map((item) => [item.id, item.ended])).toEqual([
      [record(1).id, false],
      [record(0).id, true],
    ]);
    expect([...store.records.keys()]).toEqual([record(1).id]);
    expect(isRetained({ expiresAt: NOW }, NOW + RETENTION_MS - 1)).toBe(true);
    expect(isRetained({ expiresAt: NOW }, NOW + RETENTION_MS)).toBe(false);
  });

  it.each([
    ['forgets it', (sent: SentPage, id: string) => sent.forget(id)],
    ['forgets every message', (sent: SentPage) => sent.forgetAll()],
  ] as const)('keeps a record gone when the page %s while its status loads', async (_action, forget) => {
    const { api, record, store, sent } = await setup({}, 1);
    const { id, manageKey } = record(0);
    await api.cancel(parseId(id), manageKey);
    const gate = Promise.withResolvers<undefined>();
    const asked = Promise.withResolvers<undefined>();
    const status = api.status.bind(api);
    api.status = async (messageId, key) => {
      const answer = await status(messageId, key);
      asked.resolve(undefined);
      await gate.promise;
      return answer;
    };
    const loading = sent.load();
    await asked.promise;
    await forget(sent, id);
    gate.resolve(undefined);
    await loading;
    expect(store.records.has(id)).toBe(false);
    expect(sent.items()).toEqual([]);
    await sent.load();
    expect(sent.items()).toEqual([]);
  });
});

function missing(message: string): never {
  throw new Error(message);
}
