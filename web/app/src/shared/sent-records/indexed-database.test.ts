import { afterEach, describe, expect, it, vi } from 'vitest';

import { browserDatabases, committed, databaseExists, openDatabase, requestResult } from './indexed-database.ts';

function opening() {
  const close = vi.fn();
  const database = { close } as unknown as IDBDatabase;
  const request = Object.assign(new EventTarget(), { result: database, error: null as DOMException | null });
  const open = vi.fn(() => request);
  const factory = { open } as unknown as IDBFactory;
  return { close, database, request, open, factory };
}

async function settled(promise: Promise<unknown>): Promise<unknown> {
  try {
    return await promise;
  } catch (error) {
    return error;
  }
}

describe('IndexedDB lifecycle', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('detects unavailable storage and enumerates databases without creating one', async () => {
    vi.stubGlobal('indexedDB', undefined);
    expect(browserDatabases()).toBeUndefined();
    await expect(databaseExists(undefined, 'nylzo')).resolves.toBe(false);
    const databases = vi.fn().mockResolvedValue([{ name: 'nylzo' }]);
    const factory = { databases } as unknown as IDBFactory;
    vi.stubGlobal('indexedDB', factory);
    expect(browserDatabases()).toBe(factory);
    await expect(databaseExists(factory, 'nylzo')).resolves.toBe(true);
    await expect(databaseExists(factory, 'other')).resolves.toBe(false);
    await expect(openDatabase(undefined, 'nylzo', vi.fn())).rejects.toMatchObject({ name: 'StorageUnavailableError' });
  });

  it('creates stores only during upgrade and resolves after a successful open', async () => {
    const { request, database, factory, open } = opening();
    const upgrade = vi.fn();
    const pending = openDatabase(factory, 'nylzo', upgrade);
    expect(open).toHaveBeenCalledExactlyOnceWith('nylzo', 1);
    expect(upgrade).not.toHaveBeenCalled();
    request.dispatchEvent(new Event('upgradeneeded'));
    expect(upgrade).toHaveBeenCalledExactlyOnceWith(database);
    request.dispatchEvent(new Event('success'));
    await expect(pending).resolves.toBe(database);
  });

  it.each(['blocked', 'error', 'timeout'])(
    'closes a database that opens after the attempt was abandoned: %s',
    async (failure) => {
      vi.useFakeTimers();
      const { request, factory, close } = opening();
      const pending = openDatabase(factory, 'nylzo', vi.fn());
      const rejected = settled(pending);
      if (failure === 'timeout') {
        await vi.advanceTimersByTimeAsync(2000);
      } else {
        request.dispatchEvent(new Event(failure));
      }
      expect(await rejected).toHaveProperty('name', 'StorageUnavailableError');
      request.dispatchEvent(new Event('success'));
      expect(close).toHaveBeenCalledOnce();
      expect(vi.getTimerCount()).toBe(0);
    },
  );

  it('preserves the underlying database error', async () => {
    const { request, factory } = opening();
    request.error = new DOMException('storage denied', 'SecurityError');
    const pending = openDatabase(factory, 'nylzo', vi.fn());
    request.dispatchEvent(new Event('error'));
    await expect(pending).rejects.toBe(request.error);
  });

  it('resolves request results and propagates both explicit and unspecified request errors', async () => {
    const request = Object.assign(new EventTarget(), { result: ['synthetic'], error: null as DOMException | null });
    const success = requestResult(request as unknown as IDBRequest<string[]>);
    request.dispatchEvent(new Event('success'));
    await expect(success).resolves.toEqual(['synthetic']);
    for (const error of [null, new DOMException('denied')]) {
      request.error = error;
      const result = requestResult(request as unknown as IDBRequest<string[]>);
      request.dispatchEvent(new Event('error'));
      await expect(result).rejects.toHaveProperty('name', error?.name ?? 'StorageUnavailableError');
    }
  });

  it('resolves only after a transaction commits', async () => {
    const transaction = Object.assign(new EventTarget(), { error: null });
    const result = committed(transaction as unknown as IDBTransaction);
    transaction.dispatchEvent(new Event('complete'));
    await expect(result).resolves.toBeUndefined();
  });

  it.each(['error', 'abort'])('rejects a transaction that cannot commit: %s', async (event) => {
    const transaction = Object.assign(new EventTarget(), { error: null });
    const result = committed(transaction as unknown as IDBTransaction);
    transaction.dispatchEvent(new Event(event));
    await expect(result).rejects.toHaveProperty('name', 'StorageUnavailableError');
  });
});
