import { idToString, KDF_NONE, newRequestKey, parseId } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import type { SentStore } from '../../shared/sent-records/sent-store.ts';

import { ApiError } from '../../shared/api/api-errors.ts';
import { fakeApi, type FakeApi, type FakeOptions } from '../../shared/api/fake-api.ts';
import { memoryStore } from '../../shared/sent-records/memory-sent-store.ts';
import { isRetained, RETENTION_MS, type SentRecord } from '../../shared/sent-records/sent-record.ts';
import { newGate } from '../../shared/testing/gate.ts';
import { assess, createSentPage, type SentPage, type SentView } from './sent-page.ts';

const NOW = Date.parse('2026-10-23T12:00:00Z');

async function remembered(api: FakeApi, createdAt: number, opensMax = 1): Promise<SentRecord> {
  const { create } = await api.instance();
  const manageKey = await newRequestKey();
  await api.create({
    id: create.id,
    idToken: create.idToken,
    readKey: new Uint8Array(32),
    manageKey: manageKey.publicKey,
    kdfId: KDF_NONE,
    opensMax,
    ttlSeconds: 3600,
    object: new Uint8Array(0),
  });
  return { id: idToString(create.id), createdAt, expiresAt: NOW + 3_600_000, manageKey };
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
  const record = (index: number): SentRecord => records[index] ?? expect.unreachable(`no record ${index}`);
  const storedRecord = (id: string): SentRecord => store.records.get(id) ?? expect.unreachable(`${id} is not stored`);
  return { api, records, record, storedRecord, store, sent: page(api, store) };
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

  it('shows a message the server no longer knows and a failed status separately', async () => {
    const { sent } = await setup({ failStatus: [new ApiError(404, 'not_found'), new ApiError(500, 'internal')] });
    await sent.load();
    expect(sent.items().map((item) => item.state.kind)).toEqual(['unknown', 'failed']);
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
    expect(await sent.cancel(id)).toEqual({ outcome: 'ended' });
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
    expect(sent.last()).toEqual({ view: 'list', items: [] });
    expect(store.records.size).toBe(0);
  });

  it('says so when the browser does not let it store anything', async () => {
    const sent = page(fakeApi(), memoryStore([], true));
    await sent.load();
    expect(sent.last()).toEqual({ view: 'unavailable' });
  });

  it('treats only a live, unexpired message without an end as cancellable', () => {
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
      { ...status, expiresAt: '2026-10-23T12:00:00Z' },
    ];
    expect(endedStatuses.map((ended) => assess({ kind: 'loaded', status: ended }, later, NOW))).toEqual(
      endedStatuses.map(() => ({ ended: true, cancellable: false })),
    );
    expect(assess({ kind: 'loaded', status: withoutExpiry }, NOW, NOW)).toEqual({ ended: true, cancellable: false });
    expect(assess({ kind: 'unknown' }, later, NOW)).toEqual({ ended: false, cancellable: false });
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

  it('keeps a record until 7 days after the message ended, at the latest after expiry', async () => {
    const { record, storedRecord, sent } = await setup({}, 1);
    await sent.load();
    await sent.cancel(record(0).id);
    const ended = storedRecord(record(0).id);
    expect(ended.endedAt).toBe(Date.parse('2026-10-23T11:00:00Z'));
    expect(isRetained(ended, Date.parse('2026-10-30T10:59:59Z'))).toBe(true);
    expect(isRetained(ended, Date.parse('2026-10-30T11:00:00Z'))).toBe(false);
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
    const gate = newGate();
    const asked = newGate();
    const status = api.status.bind(api);
    api.status = async (messageId, key) => {
      const answer = await status(messageId, key);
      asked.open();
      await gate.opened;
      return answer;
    };
    const loading = sent.load();
    await asked.opened;
    await forget(sent, id);
    gate.open();
    await loading;
    expect(store.records.has(id)).toBe(false);
    expect(sent.items()).toEqual([]);
    await sent.load();
    expect(sent.items()).toEqual([]);
  });
});
