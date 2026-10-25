export type KdfRequest =
  | { readonly kind: 'scrypt'; readonly passphrase: string; readonly saltPw: Uint8Array<ArrayBuffer> }
  | {
      readonly kind: 'pow';
      readonly challenge: string;
      readonly id: Uint8Array<ArrayBuffer>;
      readonly difficulty: number;
      readonly deadline: number;
    };

export type KdfResponse =
  | { readonly kind: 'progress'; readonly fraction: number }
  | { readonly kind: 'key'; readonly key: Uint8Array<ArrayBuffer> }
  | { readonly kind: 'token'; readonly token: string }
  | { readonly kind: 'error'; readonly timeout: boolean };

export class KdfWorkerError extends Error {
  override readonly name = 'KdfWorkerError';

  constructor(message = 'the key derivation worker failed') {
    super(message);
  }
}

const PROGRESS_STEP = 0.01;
export const PROGRESS_HEARTBEAT_MS = 1000;

export function throttledProgress(
  postProgress: (fraction: number) => void,
  now: () => number = Date.now,
): (fraction: number) => void {
  let postedFraction = -Infinity;
  let postedAt = -Infinity;
  return (fraction) => {
    const finished = fraction >= 1 && postedFraction < 1;
    const due = fraction - postedFraction >= PROGRESS_STEP || now() - postedAt >= PROGRESS_HEARTBEAT_MS;
    if (!finished && !due) {
      return;
    }
    postedFraction = fraction;
    postedAt = now();
    postProgress(fraction);
  };
}
