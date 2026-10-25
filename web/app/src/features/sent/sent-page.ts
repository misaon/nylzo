import { parseId } from '@nylzo/core';

import type { Api, Status } from '../../shared/api/api.ts';

import { ApiError } from '../../shared/api/api-errors.ts';
import { retainedRecords, type SentStore } from '../../shared/sent-records/sent-store.ts';
import { type SentRecord } from '../../shared/sent-records/sent-record.ts';

export type ItemState =
  | { readonly kind: 'loading' }
  | { readonly kind: 'loaded'; readonly status: Status }
  | { readonly kind: 'unknown' }
  | { readonly kind: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly kind: 'failed' };

export interface SentItem {
  readonly id: string;
  readonly createdAt: number;
  readonly expiresAt: number;
  readonly state: ItemState;
  readonly ended: boolean;
  readonly cancellable: boolean;
}

export type SentView =
  | { readonly view: 'loading' }
  | { readonly view: 'unavailable' }
  | { readonly view: 'list'; readonly items: readonly SentItem[] };

export type CancelOutcome =
  | { readonly outcome: 'cancelled' }
  | { readonly outcome: 'ended' }
  | { readonly outcome: 'unknown' }
  | { readonly outcome: 'retry_later'; readonly retryAfter?: number | undefined }
  | { readonly outcome: 'failed' }
  | { readonly outcome: 'ignored' };

export interface SentDependencies {
  readonly api: Api;
  readonly store: SentStore;
  readonly now: () => number;
}

export interface SentPage {
  load(): Promise<void>;
  refresh(id: string): Promise<void>;
  cancel(id: string): Promise<CancelOutcome>;
  forget(id: string): Promise<void>;
  forgetAll(): Promise<void>;
}

export function assess(state: ItemState, expiresAt: number, now: number): Pick<SentItem, 'ended' | 'cancellable'> {
  if (state.kind !== 'loaded') {
    return { ended: false, cancellable: false };
  }
  const { status } = state;
  const expires = status.expiresAt === undefined ? expiresAt : Date.parse(status.expiresAt);
  const ended =
    status.endedAt !== undefined || (status.state !== 'available' && status.state !== 'opened') || expires <= now;
  return { ended, cancellable: !ended };
}

function failedState(error: unknown): ItemState {
  if (error instanceof ApiError) {
    if (error.kind === 'not_found') {
      return { kind: 'unknown' };
    }
    if (error.kind === 'rate_limited' || error.kind === 'unavailable') {
      return { kind: 'retry_later', retryAfter: error.retryAfter };
    }
  }
  return { kind: 'failed' };
}

function cancelOutcome(error: unknown): CancelOutcome {
  if (error instanceof ApiError) {
    switch (error.kind) {
      case 'gone': {
        return { outcome: 'ended' };
      }
      case 'not_found': {
        return { outcome: 'unknown' };
      }
      case 'rate_limited':
      case 'unavailable': {
        return { outcome: 'retry_later', retryAfter: error.retryAfter };
      }
      default: {
        return { outcome: 'failed' };
      }
    }
  }
  return { outcome: 'failed' };
}

export function createSentPage(dependencies: SentDependencies, notify: (view: SentView) => void): SentPage {
  let records: SentRecord[] = [];
  let states = new Map<string, ItemState>();
  let generation = 0;
  const cancelling = new Set<string>();

  function show(): void {
    const now = dependencies.now();
    notify({
      view: 'list',
      items: records.map((record) => {
        const state = states.get(record.id) ?? { kind: 'loading' };
        return {
          id: record.id,
          createdAt: record.createdAt,
          expiresAt: record.expiresAt,
          state,
          ...assess(state, record.expiresAt, now),
        };
      }),
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
    show();
  }

  async function stored(id: string): Promise<SentRecord | undefined> {
    const found = (await dependencies.store.list()).find((record) => record.id === id);
    if (!found) {
      drop(id);
    }
    return found;
  }

  async function rememberEnd(record: SentRecord, status: Status): Promise<void> {
    if (status.endedAt === undefined || record.endedAt !== undefined) {
      return;
    }
    const ended = await dependencies.store.markEnded(record.id, Date.parse(status.endedAt)).catch(() => {});
    if (ended) {
      records = records.map((candidate) => (candidate.id === record.id ? ended : candidate));
    }
  }

  async function fetchStatus(id: string): Promise<ItemState | undefined> {
    try {
      const record = await stored(id);
      if (!record) {
        return undefined;
      }
      const status = await dependencies.api.status(parseId(id), record.manageKey);
      await rememberEnd(record, status);
      return { kind: 'loaded', status };
    } catch (error) {
      return failedState(error);
    }
  }

  async function load(): Promise<void> {
    const current = ++generation;
    notify({ view: 'loading' });
    try {
      records = await retainedRecords(dependencies.store, dependencies.now());
    } catch {
      records = [];
      notify({ view: 'unavailable' });
      return;
    }
    states = new Map();
    show();
    for (const { id } of [...records]) {
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
    let outcome: CancelOutcome;
    try {
      const record = await stored(id);
      if (!record) {
        return { outcome: 'ignored' };
      }
      await dependencies.api.cancel(parseId(id), record.manageKey);
      outcome = { outcome: 'cancelled' };
    } catch (error) {
      outcome = cancelOutcome(error);
    } finally {
      cancelling.delete(id);
    }
    if (outcome.outcome === 'unknown') {
      setState(id, { kind: 'unknown' });
    } else if (outcome.outcome !== 'retry_later' && outcome.outcome !== 'failed') {
      await refresh(id);
    }
    return outcome;
  }

  async function forget(id: string): Promise<void> {
    await dependencies.store.remove(id);
    drop(id);
  }

  async function forgetAll(): Promise<void> {
    generation++;
    await dependencies.store.clear();
    records = [];
    states = new Map();
    show();
  }

  return { load, refresh, cancel, forget, forgetAll };
}
