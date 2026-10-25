import { PowTimeoutError } from '@nylzo/core';

import { type KdfRequest, type KdfResponse, KdfWorkerError } from './kdf-protocol.ts';
import kdfWorkerUrl from './kdf-worker.ts?worker&url';

export type Progress = (fraction: number) => void;

const SILENCE_LIMIT_MS = 30_000;

let kdfPolicy: TrustedTypePolicy | undefined;

function workerScriptUrl(): string {
  if (typeof trustedTypes === 'undefined') {
    return kdfWorkerUrl;
  }
  // eslint-disable-next-line unicorn/no-top-level-assignment-in-function -- Trusted Types refuses a second policy named nylzo-kdf, so later workers reuse the first.
  kdfPolicy ??= trustedTypes.createPolicy('nylzo-kdf', {
    createScriptURL(input) {
      if (input !== kdfWorkerUrl) {
        throw new TypeError('unexpected worker url');
      }
      return input;
    },
  });
  // Worker accepts a TrustedScriptURL; the DOM typings only know strings.
  return kdfPolicy.createScriptURL(kdfWorkerUrl) as unknown as string;
}

async function runWorker(request: KdfRequest, onProgress: Progress): Promise<KdfResponse> {
  const worker = new Worker(workerScriptUrl(), { type: 'module', name: 'nylzo-kdf' });
  return new Promise((resolve, reject) => {
    let silenceTimer: ReturnType<typeof setTimeout> | undefined;
    const finish = (settle: () => void) => {
      clearTimeout(silenceTimer);
      worker.terminate();
      settle();
    };
    const fail = (error: Error) => finish(() => reject(error));
    const restartSilenceTimer = () => {
      clearTimeout(silenceTimer);
      silenceTimer = setTimeout(
        () => fail(new KdfWorkerError('the key derivation worker stopped responding')),
        SILENCE_LIMIT_MS,
      );
    };
    worker.addEventListener('message', ({ data }: MessageEvent<KdfResponse>) => {
      if (data.kind === 'progress') {
        restartSilenceTimer();
        onProgress(data.fraction);
      } else if (data.kind === 'error') {
        fail(data.timeout ? new PowTimeoutError() : new KdfWorkerError());
      } else {
        finish(() => resolve(data));
      }
    });
    worker.addEventListener('error', () => fail(new KdfWorkerError()));
    worker.addEventListener('messageerror', () => fail(new KdfWorkerError()));
    restartSilenceTimer();
    worker.postMessage(request);
  });
}

export async function scryptInWorker(
  passphrase: string,
  passphraseSalt: Uint8Array<ArrayBuffer>,
  onProgress: Progress,
): Promise<Uint8Array<ArrayBuffer>> {
  const response = await runWorker({ kind: 'scrypt', passphrase, passphraseSalt }, onProgress);
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
  onProgress: Progress,
): Promise<string> {
  const response = await runWorker({ kind: 'pow', challenge, id, difficulty, deadline }, onProgress);
  if (response.kind !== 'token') {
    throw new KdfWorkerError();
  }
  return response.token;
}
