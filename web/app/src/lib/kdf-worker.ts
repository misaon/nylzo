import { PowTimeoutError, solvePow } from '@nylzo/core';
import { scryptPassphraseKey } from '@nylzo/core/scrypt';

import { throttledProgress, type KdfRequest, type KdfResponse } from './kdf-protocol.ts';

function reply(message: KdfResponse, transfer: Transferable[] = []): void {
  self.postMessage(message, { transfer });
}

function handle(request: KdfRequest): void {
  const onProgress = throttledProgress((fraction) => reply({ kind: 'progress', fraction }));
  if (request.kind === 'scrypt') {
    const key = scryptPassphraseKey(request.passphrase, request.saltPw, onProgress);
    reply({ kind: 'key', key }, [key.buffer]);
    return;
  }
  const expected = 2 ** request.difficulty;
  const token = solvePow(request.challenge, request.id, request.difficulty, {
    deadline: request.deadline,
    onAttempt: (counter) => onProgress(Math.min(counter / expected, 1)),
  });
  reply({ kind: 'token', token });
}

self.addEventListener('message', (event: MessageEvent<KdfRequest>) => {
  try {
    handle(event.data);
  } catch (error) {
    reply({ kind: 'error', timeout: error instanceof PowTimeoutError });
  }
});
