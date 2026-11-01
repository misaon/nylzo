import { PowTimeoutError } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { KdfResponse } from './kdf-protocol.ts';

import { KdfWorkerError } from './kdf-protocol.ts';
import { scryptInWorker, solvePowInWorker } from './kdf.ts';
import { startWorker } from './worker-source.ts';

vi.mock('./worker-source.ts', () => ({ startWorker: vi.fn() }));

async function settled(promise: Promise<unknown>): Promise<unknown> {
  try {
    return await promise;
  } catch (error) {
    return error;
  }
}

describe('worker lifecycle', () => {
  let worker: EventTarget & { postMessage: ReturnType<typeof vi.fn> };
  const stop = vi.fn();
  const progress = vi.fn();
  const salt = new Uint8Array(16).fill(1);

  function reply(data: KdfResponse): void {
    worker.dispatchEvent(new MessageEvent('message', { data }));
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    worker = Object.assign(new EventTarget(), { postMessage: vi.fn() });
    vi.mocked(startWorker).mockResolvedValue({ worker: worker as unknown as Worker, stop });
  });
  afterEach(() => vi.useRealTimers());

  it('forwards the passphrase and salt and stops the worker after receiving the key', async () => {
    const key = new Uint8Array(32).fill(7);
    const result = scryptInWorker('synthetic passphrase', salt, progress);
    await Promise.resolve();
    expect(worker.postMessage).toHaveBeenCalledExactlyOnceWith({
      kind: 'scrypt',
      passphrase: 'synthetic passphrase',
      passphraseSalt: salt,
    });
    reply({ kind: 'progress', fraction: 0.5 });
    expect(progress).toHaveBeenCalledExactlyOnceWith(0.5);
    reply({ kind: 'key', key });
    await expect(result).resolves.toBe(key);
    expect(stop).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('forwards proof-of-work parameters and returns the token', async () => {
    const id = new Uint8Array(16).fill(2);
    const result = solvePowInWorker('challenge', id, 4, 1234, progress);
    await Promise.resolve();
    expect(worker.postMessage).toHaveBeenCalledExactlyOnceWith({
      kind: 'pow',
      challenge: 'challenge',
      id,
      difficulty: 4,
      deadline: 1234,
    });
    reply({ kind: 'token', token: 'synthetic-token' });
    await expect(result).resolves.toBe('synthetic-token');
    expect(stop).toHaveBeenCalledOnce();
  });

  it.each(['error', 'messageerror'])('cleans up on a worker %s event', async (type) => {
    const result = scryptInWorker('phrase', salt, progress);
    const failed = settled(result);
    await Promise.resolve();
    worker.dispatchEvent(new Event(type));
    expect(await failed).toBeInstanceOf(KdfWorkerError);
    expect(stop).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([true, false])('distinguishes proof-of-work deadline expiry from worker failure: %s', async (timeout) => {
    const result = solvePowInWorker('challenge', new Uint8Array(16), 1, 1, progress);
    const failed = settled(result);
    await Promise.resolve();
    reply({ kind: 'error', timeout });
    expect(await failed).toBeInstanceOf(timeout ? PowTimeoutError : KdfWorkerError);
    expect(stop).toHaveBeenCalledOnce();
  });

  it('renews the silence timeout on progress, then stops a stalled worker', async () => {
    const result = scryptInWorker('phrase', salt, progress);
    const failed = settled(result);
    await Promise.resolve();
    vi.advanceTimersByTime(29_000);
    reply({ kind: 'progress', fraction: 0.1 });
    vi.advanceTimersByTime(29_000);
    expect(stop).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(await failed).toHaveProperty('message', 'the key derivation worker stopped responding');
    expect(stop).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('rejects mismatched response types instead of treating them as keys or tokens', async () => {
    const key = scryptInWorker('phrase', salt, progress);
    const rejectedKey = settled(key);
    await Promise.resolve();
    reply({ kind: 'token', token: 'wrong-kind' });
    expect(await rejectedKey).toBeInstanceOf(KdfWorkerError);
    worker = Object.assign(new EventTarget(), { postMessage: vi.fn() });
    vi.mocked(startWorker).mockResolvedValue({ worker: worker as unknown as Worker, stop });
    const token = solvePowInWorker('challenge', new Uint8Array(16), 1, 1, progress);
    const rejectedToken = settled(token);
    await Promise.resolve();
    reply({ kind: 'key', key: new Uint8Array(32) });
    expect(await rejectedToken).toBeInstanceOf(KdfWorkerError);
    expect(stop).toHaveBeenCalledTimes(2);
  });

  it('propagates startup failure without leaving a silence timer behind', async () => {
    vi.mocked(startWorker).mockRejectedValue(new Error('integrity refused'));
    await expect(scryptInWorker('phrase', salt, progress)).rejects.toThrow('integrity refused');
    expect(stop).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });
});
