import { KDF_SCRYPT, PowTimeoutError, derivePassphraseKey, solvePow } from '@nylzo/core';

import type { KdfRequest, KdfResponse } from './kdf-protocol.ts';

function reply(message: KdfResponse, transfer: Transferable[] = []): void {
  self.postMessage(message, { transfer });
}

async function handle(request: KdfRequest): Promise<void> {
  const onProgress = (fraction: number) => reply({ kind: 'progress', fraction });
  if (request.kind === 'scrypt') {
    const key = await derivePassphraseKey(KDF_SCRYPT, request.passphrase, request.saltPw, { blocking: true, onProgress });
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
  handle(event.data).catch((error: unknown) => reply({ kind: 'error', timeout: error instanceof PowTimeoutError }));
});
