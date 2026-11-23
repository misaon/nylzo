import { encodeBase64url } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { KdfResponse } from './kdf-protocol.ts';

vi.mock(import('@nylzo/core/scrypt'), () => ({
  stretchScrypt: (message: Uint8Array, onProgress?: (fraction: number) => void) => {
    onProgress?.(1);
    return new Uint8Array(64).fill(message[0] ?? 0);
  },
}));

const ORIGIN = 'https://nylzo.test';
const posted: { readonly message: KdfResponse; readonly transfer: readonly Transferable[] }[] = [];
let deliver: ((event: MessageEvent<unknown>) => void) | undefined;

function send(data: unknown, origin = ''): void {
  deliver?.(new MessageEvent('message', { data, origin }));
}

beforeEach(async () => {
  posted.length = 0;
  vi.resetModules();
  vi.stubGlobal('location', { origin: ORIGIN });
  vi.stubGlobal('postMessage', (message: KdfResponse, options: { transfer: Transferable[] }) => {
    posted.push({ message, transfer: options.transfer });
  });
  vi.stubGlobal('addEventListener', (type: string, listener: (event: MessageEvent<unknown>) => void) => {
    if (type === 'message') {
      deliver = listener;
    }
  });
  await import('./kdf-worker.ts');
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const challenge = encodeBase64url(new Uint8Array(36));

describe('the key derivation worker', () => {
  it('stretches the OPRF output, wipes it and transfers the key', () => {
    const message = new Uint8Array(64).fill(3);
    send({ kind: 'stretch', message });
    expect(message.every((byte) => byte === 0)).toBe(true);
    const [progress, answer] = posted;
    expect(progress?.message).toEqual({ kind: 'progress', fraction: 1 });
    expect(answer?.message).toEqual({ kind: 'key', key: new Uint8Array(64).fill(3) });
    expect(answer?.transfer).toHaveLength(1);
  });

  it('solves a proof of work from the page of its own origin', () => {
    send({ kind: 'pow', challenge, id: new Uint8Array(16), difficulty: 0, budgetMs: 60_000 }, ORIGIN);
    expect(posted.map(({ message }) => message.kind)).toEqual(['progress', 'token']);
  });

  it('reports a proof of work that runs out of its budget as a timeout', () => {
    vi.spyOn(Date, 'now').mockReturnValueOnce(0).mockReturnValue(Number.MAX_SAFE_INTEGER);
    send({ kind: 'pow', challenge, id: new Uint8Array(16), difficulty: 7, budgetMs: 1 });
    expect(posted.map(({ message }) => message)).toEqual([{ kind: 'error', timeout: true }]);
  });

  it('answers a malformed request with an error that is no timeout', () => {
    send({ kind: 'stretch', message: new Uint8Array(3) });
    expect(posted.map(({ message }) => message)).toEqual([{ kind: 'error', timeout: false }]);
  });

  it('ignores a message from another origin', () => {
    send({ kind: 'stretch', message: new Uint8Array(64) }, 'https://elsewhere.test');
    expect(posted).toEqual([]);
  });
});
