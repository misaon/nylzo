import { PowTimeoutError, solvePow } from '@nylzo/core';
import { stretchScrypt } from '@nylzo/core/scrypt';

import { isKdfRequest, throttledProgress } from './kdf-protocol.ts';
import type { KdfRequest, KdfResponse } from './kdf-protocol.ts';

// oxlint-disable-next-line typescript/prefer-readonly-parameter-types -- postMessage requires a mutable transfer list in the platform API.
function reply(message: KdfResponse, transfer: Transferable[] = []): void {
  postMessage(message, { transfer });
}

function handle(request: KdfRequest): void {
  const onProgress = throttledProgress((fraction) => reply({ kind: 'progress', fraction }));
  if (request.kind === 'stretch') {
    const key = stretchScrypt(request.message, onProgress);
    request.message.fill(0);
    reply({ kind: 'key', key }, [key.buffer]);
    return;
  }
  const expected = 2 ** request.difficulty;
  const token = solvePow(request.challenge, request.id, request.difficulty, {
    budgetMs: request.budgetMs,
    onAttempt: (counter) => onProgress(Math.min(counter / expected, 1)),
  });
  reply({ kind: 'token', token });
}

addEventListener('message', (event: MessageEvent<unknown>) => {
  // Only the page that started this dedicated worker posts to it, through the implicit port whose events carry no origin.
  if (event.origin !== '' && event.origin !== location.origin) {
    return;
  }
  try {
    if (!isKdfRequest(event.data)) {
      throw new TypeError('malformed key derivation request');
    }
    handle(event.data);
  } catch (error) {
    reply({ kind: 'error', timeout: error instanceof PowTimeoutError });
  }
});
