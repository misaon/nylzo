import { PowTimeoutError } from '@nylzo/core';

import { type KdfRequest, type KdfResponse, KdfWorkerError } from './kdf-protocol.ts';
import { startWorker } from './worker-source.ts';

export type Progress = (fraction: number) => void;

const SILENCE_LIMIT_MS = 30_000;

async function runWorker(request: KdfRequest, onProgress: Progress): Promise<KdfResponse> {
  const { worker, stop } = await startWorker();
  return new Promise((resolve, reject) => {
    let silenceTimer: ReturnType<typeof setTimeout> | undefined;
    const finish = (settle: () => void) => {
      clearTimeout(silenceTimer);
      stop();
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
