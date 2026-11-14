import { parseId } from '@nylzo/core';

import { isApiError, shouldRetryLater } from '../../shared/api/api-errors.ts';
import type { Api, Status } from '../../shared/api/api.ts';
import type { SentRecord } from '../../shared/sent-records/sent-record.ts';
import { retainedRecords } from '../../shared/sent-records/sent-store.ts';
import type { SentStore } from '../../shared/sent-records/sent-store.ts';

export type ItemState =
  | { readonly kind: 'loading' }
  | { readonly kind: 'loaded'; readonly status: Status }
  | { readonly kind: 'unknown' }
  | { readonly kind: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly kind: 'failed' };

export interface SentItem {
  readonly id: string;
  readonly label?: string | undefined;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly state: ItemState;
  readonly ended: boolean;
  readonly cancellable: boolean;
  readonly session: boolean;
}

export type SentView =
  | { readonly view: 'loading' }
  | { readonly view: 'unavailable' }
  | { readonly view: 'list'; readonly items: readonly SentItem[]; readonly storageFailed: boolean };

export type CancelOutcome =
  | { readonly outcome: 'cancelled' }
  | { readonly outcome: 'ended' }
  | { readonly outcome: 'unknown' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

export interface SentDependencies {
  readonly api: Api;
  readonly session: SentStore;
  readonly browser?: SentStore | undefined;
  readonly now: () => number;
}

export interface SentPage {
  load(): Promise<void>;
  refresh(id: string): Promise<void>;
  cancel(id: string): Promise<CancelOutcome>;
  forget(id: string): Promise<void>;
  forgetAll(): Promise<void>;
  stop(): void;
}

// Only the server's own clock ends a message; a skewed local clock must not cost the sender the key or the cancel button.
function endedOnServer(status: Status): boolean {
  return status.endedAt !== undefined || (status.state !== 'available' && status.state !== 'opened');
}

export function assess(state: ItemState, expiresAt: number, now: number): Pick<SentItem, 'ended' | 'cancellable'> {
  if (state.kind === 'failed' || state.kind === 'retry_later') {
    return { ended: false, cancellable: expiresAt > now };
  }
  if (state.kind !== 'loaded') {
    return { ended: false, cancellable: false };
  }
  const ended = endedOnServer(state.status);
  return { ended, cancellable: !ended };
}

function failedState(error: unknown): ItemState {
  if (isApiError(error, 'not_found')) {
    return { kind: 'unknown' };
  }
  if (shouldRetryLater(error)) {
    return { kind: 'retry_later', retryAfter: error.retryAfter };
  }
  return { kind: 'failed' };
}

function cancelOutcome(error: unknown): CancelOutcome {
  if (isApiError(error, 'gone')) {
    return { outcome: 'ended' };
  }
  if (isApiError(error, 'not_found')) {
    return { outcome: 'unknown' };
  }
  if (shouldRetryLater(error)) {
    return { outcome: 'retry_later', retryAfter: error.retryAfter };
  }
  return { outcome: 'failed' };
}

async function retained(store: SentStore | undefined, now: number): Promise<SentRecord[]> {
  return store ? retainedRecords(store, now) : [];
}

export function createSentPage(dependencies: SentDependencies, notify: (view: SentView) => void): SentPage {
  const { api, session, browser, now } = dependencies;
  let records: SentRecord[] = [];
  let homes = new Map<string, SentStore>();
  let states = new Map<string, ItemState>();
  let generation = 0;
  let storageFailed = false;
  let concluded = new Set<string>();
  const cancelling = new Set<string>();

  function show(): void {
    const moment = now();
    notify({
      view: 'list',
      items: records.map((record) => {
        const state = states.get(record.id) ?? { kind: 'loading' };
        return {
          id: record.id,
          label: record.label,
          createdAt: record.createdAt,
          expiresAt: record.expiresAt,
          state,
          ...(concluded.has(record.id) ? { ended: true, cancellable: false } : assess(state, record.expiresAt, moment)),
          session: homes.get(record.id) === session,
        };
      }),
      storageFailed,
    });
  }

  function listed(id: string): boolean {
    return records.some((record) => record.id === id);
  }

  function setState(id: string, state: ItemState): void {
    if (!listed(id)) {
      return;
    }

    states.set(id, state);
    show();
  }

  function drop(id: string): void {
    records = records.filter((record) => record.id !== id);
    states.delete(id);
    homes.delete(id);
    show();
  }

  async function stored(id: string): Promise<SentRecord | undefined> {
    const saved = (await homes.get(id)?.list()) ?? [];
    const found = saved.find((record) => record.id === id);
    if (!found) {
      drop(id);
    }
    return found;
  }

  // The page keeps showing an ended message, but its key and label leave storage at once; a failed removal waits for the retention purge.
  async function forgetEnded(id: string, home: SentStore | undefined): Promise<void> {
    try {
      await home?.remove(id);
    } catch {
      storageFailed = true;
    }
  }

  async function statusOf(record: SentRecord, home: SentStore | undefined): Promise<ItemState> {
    try {
      const status = await api.status(parseId(record.id), record.manageKey);
      if (endedOnServer(status)) {
        await forgetEnded(record.id, home);
      }
      return { kind: 'loaded', status };
    } catch (error) {
      return failedState(error);
    }
  }

  async function fetchStatus(id: string): Promise<ItemState | undefined> {
    const home = homes.get(id);
    let record: SentRecord | undefined;
    try {
      record = await stored(id);
    } catch (error) {
      return failedState(error);
    }
    return record && statusOf(record, home);
  }

  async function load(): Promise<void> {
    const current = ++generation;
    notify({ view: 'loading' });
    const moment = now();
    const [inSession, inBrowser] = await Promise.allSettled([retained(session, moment), retained(browser, moment)]);
    if (current !== generation) {
      return;
    }
    const kept = inSession.status === 'fulfilled' ? inSession.value : [];
    storageFailed = inBrowser.status === 'rejected';
    if (inBrowser.status === 'rejected' && kept.length === 0) {
      records = [];
      homes = new Map();
      notify({ view: 'unavailable' });
      return;
    }
    const saved = inBrowser.status === 'fulfilled' ? inBrowser.value : [];
    homes = new Map<string, SentStore>(kept.map((record) => [record.id, session]));
    const fromBrowser = saved.filter((record) => !homes.has(record.id));
    if (browser) {
      for (const record of fromBrowser) {
        homes.set(record.id, browser);
      }
    }
    records = [...kept, ...fromBrowser].toSorted((first, second) => second.createdAt - first.createdAt);
    states = new Map();
    concluded = new Set();
    show();
    // The loop holds ids only, so a page that stops keeps no key alive while a request waits.
    const ids = records.map((record) => record.id);
    for (const id of ids) {
      if (current !== generation) {
        return;
      }
      const state = await fetchStatus(id);
      if (current !== generation || state === undefined) {
        continue;
      }
      setState(id, state);
      if (state.kind === 'retry_later') {
        for (const record of records) {
          if (!states.has(record.id)) {
            states.set(record.id, state);
          }
        }
        show();
        return;
      }
    }
  }

  async function refresh(id: string): Promise<void> {
    if (!listed(id)) {
      return;
    }
    setState(id, { kind: 'loading' });
    const state = await fetchStatus(id);
    if (state) {
      setState(id, state);
    }
  }

  async function cancel(id: string): Promise<CancelOutcome> {
    if (!listed(id) || cancelling.has(id)) {
      return { outcome: 'ignored' };
    }
    cancelling.add(id);
    const home = homes.get(id);
    let outcome: CancelOutcome;
    let record: SentRecord | undefined;
    try {
      record = await stored(id);
      if (!record) {
        return { outcome: 'ignored' };
      }
      await api.cancel(parseId(id), record.manageKey);
      outcome = { outcome: 'cancelled' };
    } catch (error) {
      outcome = cancelOutcome(error);
    } finally {
      cancelling.delete(id);
    }
    if (outcome.outcome === 'unknown') {
      setState(id, { kind: 'unknown' });
    } else if (record && (outcome.outcome === 'cancelled' || outcome.outcome === 'ended')) {
      concluded.add(id);
      await forgetEnded(id, home);
      setState(id, { kind: 'loading' });
      setState(id, await statusOf(record, home));
    }
    return outcome;
  }

  async function forget(id: string): Promise<void> {
    const home = homes.get(id);
    await (home ? home.remove(id) : Promise.all([session.remove(id), browser?.remove(id)]));
    drop(id);
  }

  function stop(): void {
    generation++;
    storageFailed = false;
    concluded = new Set();
    records = [];
    states = new Map();
    homes = new Map();
    notify({ view: 'loading' });
  }

  async function forgetAll(): Promise<void> {
    generation++;
    await session.clear();
    records = records.filter((record) => homes.get(record.id) !== session);
    show();
    await browser?.clear();
    records = [];
    states = new Map();
    homes = new Map();
    show();
  }

  return { load, refresh, cancel, forget, forgetAll, stop };
}
