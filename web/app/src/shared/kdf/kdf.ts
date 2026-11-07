import { PowTimeoutError } from '@nylzo/core';

import { isKdfResponse, KdfWorkerError } from './kdf-protocol.ts';
import type { KdfRequest, KdfResponse } from './kdf-protocol.ts';
import { startWorker } from './worker-source.ts';

export type Progress = (fraction: number) => void;

const SILENCE_LIMIT_MS = 30_000;

const runningWorkers = new Set<() => void>();
let pageVisit = 0;

// A passphrase must not stay in a worker after the page is left, even mid-derivation.
export function stopRunningWorkers(): void {
  pageVisit++;
  for (const cancel of runningWorkers) {
    cancel();
  }
}

async function runWorker(requestOnStart: () => KdfRequest, onProgress: Progress): Promise<KdfResponse> {
  const visit = pageVisit;
  const { worker, stop } = await startWorker();
  if (visit !== pageVisit) {
    stop();
    throw new KdfWorkerError('the page was left');
  }
  return new Promise((resolve, reject) => {
    let silenceTimer: ReturnType<typeof setTimeout> | undefined;
    let settled = false;
    const finish = (settle: () => void) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(silenceTimer);
      runningWorkers.delete(cancel);
      stop();
      settle();
    };
    const fail = (error: Error) => finish(() => reject(error));
    const cancel = () => fail(new KdfWorkerError('the page was left'));
    runningWorkers.add(cancel);
    const restartSilenceTimer = () => {
      clearTimeout(silenceTimer);
      silenceTimer = setTimeout(
        () => fail(new KdfWorkerError('the key derivation worker stopped responding')),
        SILENCE_LIMIT_MS,
      );
    };
    worker.addEventListener('message', ({ data }: MessageEvent<unknown>) => {
      if (settled) {
        return;
      }
      if (!isKdfResponse(data)) {
        fail(new KdfWorkerError('the key derivation worker sent a malformed message'));
      } else if (data.kind === 'progress') {
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
    worker.postMessage(requestOnStart(), []);
  });
}

export async function scryptInWorker(
  passphrase: string,
  passphraseSalt: Uint8Array<ArrayBuffer>,
  onProgress: Progress,
): Promise<Uint8Array<ArrayBuffer>> {
  const response = await runWorker(() => ({ kind: 'scrypt', passphrase, passphraseSalt }), onProgress);
  if (response.kind !== 'key') {
    throw new KdfWorkerError();
  }
  return response.key;
}

export async function solvePowInWorker(
  challenge: string,
  id: Uint8Array<ArrayBuffer>,
  difficulty: number,
  budgetMs: number,
  onProgress: Progress,
): Promise<string> {
  // The challenge expires on the server's clock, so starting the worker spends the same budget.
  const deadline = performance.now() + budgetMs;
  const response = await runWorker(
    () => ({ kind: 'pow', challenge, id, difficulty, budgetMs: Math.max(1, deadline - performance.now()) }),
    onProgress,
  );
  if (response.kind !== 'token' || !response.token.startsWith(`${challenge}.`)) {
    throw new KdfWorkerError();
  }
  return response.token;
}
