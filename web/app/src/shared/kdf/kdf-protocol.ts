export type KdfRequest =
  | { readonly kind: 'stretch'; readonly message: Uint8Array<ArrayBuffer> }
  | {
      readonly kind: 'pow';
      readonly challenge: string;
      readonly id: Uint8Array<ArrayBuffer>;
      readonly difficulty: number;
      readonly budgetMs: number;
    };

export type KdfResponse =
  | { readonly kind: 'progress'; readonly fraction: number }
  | { readonly kind: 'key'; readonly key: Uint8Array<ArrayBuffer> }
  | { readonly kind: 'token'; readonly token: string }
  | { readonly kind: 'error'; readonly timeout: boolean };

// The OPRF output that profile 3 stretches and the stretched key are both 64 bytes (spec 2.1).
const STRETCH_LENGTH = 64;
const ID_LENGTH = 16;

function field(value: object, name: string): unknown {
  return Reflect.get(value, name);
}

function isBytes(value: unknown, length: number): value is Uint8Array<ArrayBuffer> {
  return value instanceof Uint8Array && value.length === length;
}

function isFraction(value: unknown): value is number {
  return typeof value === 'number' && value >= 0 && value <= 1;
}

function isBudget(value: unknown): value is number {
  return typeof value === 'number' && value > 0 && Number.isFinite(value);
}

const REQUEST_CHECKS = new Map<unknown, (value: object) => boolean>([
  ['stretch', (value) => isBytes(field(value, 'message'), STRETCH_LENGTH)],
  [
    'pow',
    (value) =>
      typeof field(value, 'challenge') === 'string' &&
      isBytes(field(value, 'id'), ID_LENGTH) &&
      Number.isSafeInteger(field(value, 'difficulty')) &&
      isBudget(field(value, 'budgetMs')),
  ],
]);

const RESPONSE_CHECKS = new Map<unknown, (value: object) => boolean>([
  ['progress', (value) => isFraction(field(value, 'fraction'))],
  ['key', (value) => isBytes(field(value, 'key'), STRETCH_LENGTH)],
  ['token', (value) => typeof field(value, 'token') === 'string'],
  ['error', (value) => typeof field(value, 'timeout') === 'boolean'],
]);

// Both ends check what crosses the worker boundary, so a malformed message fails closed instead of being trusted.
export function isKdfRequest(value: unknown): value is KdfRequest {
  return typeof value === 'object' && value !== null && REQUEST_CHECKS.get(field(value, 'kind'))?.(value) === true;
}

export function isKdfResponse(value: unknown): value is KdfResponse {
  return typeof value === 'object' && value !== null && RESPONSE_CHECKS.get(field(value, 'kind'))?.(value) === true;
}

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
