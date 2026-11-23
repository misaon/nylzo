import { ApiError } from '../../shared/api/api-errors.ts';

export type Problem =
  | 'already-enrolled'
  | 'busy'
  | 'cancelled'
  | 'failed'
  | 'key-type'
  | 'link'
  | 'missing'
  | 'not-allowed'
  | 'offline'
  | 'refused'
  | 'unsupported';

export class SessionEndedError extends Error {
  override readonly name = 'SessionEndedError';

  constructor() {
    super('the admin session ended');
  }
}

export class KeyTypeError extends Error {
  override readonly name = 'KeyTypeError';

  constructor() {
    super('the browser cannot hand over the public key of this passkey');
  }
}

const PASSKEY_PROBLEMS: ReadonlyMap<string, Problem> = new Map([
  ['NotAllowedError', 'cancelled'],
  ['AbortError', 'cancelled'],
  ['InvalidStateError', 'already-enrolled'],
  ['SecurityError', 'not-allowed'],
  ['NotSupportedError', 'unsupported'],
]);

export class UnreachableError extends Error {
  override readonly name = 'UnreachableError';

  constructor(cause: unknown) {
    super('the server did not answer', { cause });
  }
}

export function problemOf(error: unknown, refusal: Problem): Problem {
  if (error instanceof KeyTypeError) {
    return 'key-type';
  }
  if (error instanceof ApiError) {
    if (error.status === 429 || error.status === 503) {
      return 'busy';
    }
    if (error.status === 404) {
      return 'missing';
    }
    return error.status === 401 ? refusal : 'failed';
  }
  if (error instanceof UnreachableError) {
    return 'offline';
  }
  if (error instanceof DOMException) {
    return PASSKEY_PROBLEMS.get(error.name) ?? 'failed';
  }
  return 'failed';
}
