import { PowTimeoutError } from '@nylzo/core';

import { KdfWorkerError, type KdfRequest, type KdfResponse } from './kdf-protocol.ts';
import workerUrl from './kdf-worker.ts?worker&url';

export const SILENCE_LIMIT_MS = 30_000;

export interface WorkerOptions {
  readonly onProgress?: (fraction: number) => void;
  readonly signal?: AbortSignal;
}

let policy: TrustedTypePolicy | undefined;

function workerScriptUrl(): string {
  const factory = globalThis.trustedTypes;
  if (!factory) {
    return workerUrl;
  }
  policy ??= factory.createPolicy('nylzo-kdf', {
    createScriptURL(input) {
      if (input !== workerUrl) {
        throw new TypeError('unexpected worker url');
      }
      return input;
    },
  });
  // Worker accepts a TrustedScriptURL; the DOM typings only know strings.
  return policy.createScriptURL(workerUrl) as unknown as string;
}

function run(request: KdfRequest, options: WorkerOptions): Promise<KdfResponse> {
  options.signal?.throwIfAborted();
  const worker = new Worker(workerScriptUrl(), { type: 'module', name: 'nylzo-kdf' });
  return new Promise((resolve, reject) => {
    let silence: ReturnType<typeof setTimeout> | undefined;
    const finish = (settle: () => void) => {
      clearTimeout(silence);
      worker.terminate();
      options.signal?.removeEventListener('abort', onAbort);
      settle();
    };
    const listen = () => {
      clearTimeout(silence);
      silence = setTimeout(() => finish(() => reject(new KdfWorkerError('the key derivation worker stopped responding'))), SILENCE_LIMIT_MS);
    };
    const onAbort = () => finish(() => reject(options.signal?.reason));
    options.signal?.addEventListener('abort', onAbort, { once: true });
    worker.addEventListener('message', ({ data }: MessageEvent<KdfResponse>) => {
      if (data.kind === 'progress') {
        listen();
        options.onProgress?.(data.fraction);
      } else if (data.kind === 'error') {
        finish(() => reject(data.timeout ? new PowTimeoutError() : new KdfWorkerError()));
      } else {
        finish(() => resolve(data));
      }
    });
    worker.addEventListener('error', () => finish(() => reject(new KdfWorkerError())));
    worker.addEventListener('messageerror', () => finish(() => reject(new KdfWorkerError())));
    listen();
    worker.postMessage(request);
  });
}

export async function scryptInWorker(
  passphrase: string,
  saltPw: Uint8Array<ArrayBuffer>,
  options: WorkerOptions = {},
): Promise<Uint8Array<ArrayBuffer>> {
  const response = await run({ kind: 'scrypt', passphrase, saltPw }, options);
  if (response.kind !== 'key') {
    throw new KdfWorkerError();
  }
  return response.key;
}

export async function solvePowInWorker(
  challenge: string,
  id: Uint8Array<ArrayBuffer>,
  difficulty: number,
  deadline: number,
  options: WorkerOptions = {},
): Promise<string> {
  const response = await run({ kind: 'pow', challenge, id, difficulty, deadline }, options);
  if (response.kind !== 'token') {
    throw new KdfWorkerError();
  }
  return response.token;
}
