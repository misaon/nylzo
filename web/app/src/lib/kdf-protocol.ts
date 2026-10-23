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

export const PROGRESS_STEP = 0.01;
export const PROGRESS_HEARTBEAT_MS = 1000;

export function throttledProgress(post: (fraction: number) => void, now: () => number = Date.now): (fraction: number) => void {
  let last = Number.NEGATIVE_INFINITY;
  let postedAt = Number.NEGATIVE_INFINITY;
  return (fraction) => {
    const finished = fraction >= 1 && last < 1;
    if (finished || fraction - last >= PROGRESS_STEP || now() - postedAt >= PROGRESS_HEARTBEAT_MS) {
      last = fraction;
      postedAt = now();
      post(fraction);
    }
  };
}
