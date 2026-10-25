export const MESSAGE_STATES = ['available', 'opened', 'expired', 'cancelled', 'deleted'] as const;

export const ERROR_KINDS = [
  'bad_request',
  'contact',
  'passphrase',
  'pow',
  'id_token',
  'not_found',
  'conflict',
  'gone',
  'length_required',
  'too_large',
  'rate_limited',
  'internal',
  'unavailable',
] as const;

export type MessageState = (typeof MESSAGE_STATES)[number];

export type EndedState = Exclude<MessageState, 'available'>;

export type ApiErrorKind = (typeof ERROR_KINDS)[number] | 'unexpected';

export interface ApiErrorDetails {
  readonly retryAfter?: number | undefined;
  readonly attemptsLeft?: number | undefined;
  readonly state?: MessageState | undefined;
}

export class ApiError extends Error {
  override readonly name = 'ApiError';
  readonly status: number;
  readonly kind: ApiErrorKind;
  readonly retryAfter: number | undefined;
  readonly attemptsLeft: number | undefined;
  readonly state: MessageState | undefined;

  constructor(status: number, kind: ApiErrorKind, details: ApiErrorDetails = {}) {
    super(`api ${status} ${kind}`);
    this.status = status;
    this.kind = kind;
    this.retryAfter = details.retryAfter;
    this.attemptsLeft = details.attemptsLeft;
    this.state = details.state;
  }
}

export class ResponseError extends Error {
  override readonly name = 'ResponseError';

  constructor(message = 'the server sent a malformed response') {
    super(message);
  }
}

export class StoppedError extends Error {
  override readonly name = 'StoppedError';

  constructor() {
    super('the page stopped the request before it was sent');
  }
}

export function isApiError(error: unknown, ...kinds: readonly ApiErrorKind[]): error is ApiError {
  return error instanceof ApiError && kinds.includes(error.kind);
}

export function shouldRetryLater(error: unknown): error is ApiError {
  return isApiError(error, 'rate_limited', 'unavailable');
}

export function endedStateOf(error: ApiError): EndedState | undefined {
  return error.state === 'available' ? undefined : error.state;
}
