import { ResponseError } from '../../shared/api/api-errors.ts';
import { readBody } from '../../shared/api/object-body.ts';
import { apiError, isObject } from '../../shared/api/responses.ts';
import type { Json } from '../../shared/api/responses.ts';
import { UnreachableError } from './problems.ts';

export const ADMIN_PATH = '/api/v1/admin';
export const CHALLENGE_PATH = '/api/v1/challenge';

const TIMEOUT_MS = 20_000;
// A live answer lists up to 256 refused clients beside five minutes of samples.
const MAX_ANSWER_BYTES = 512 * 1024;

export type Fetch = typeof fetch;

type Method = 'DELETE' | 'GET' | 'POST';

export interface Transport {
  readonly send: (method: Method, path: string, options?: SendOptions) => Promise<Json>;
}

interface SendOptions {
  readonly body?: Json;
  readonly headers?: Readonly<Record<string, string>>;
}

async function readAnswer(response: Response): Promise<Json> {
  if (response.headers.get('Content-Type')?.split(';')[0]?.trim() !== 'application/json') {
    await response.body?.cancel();
    throw new ResponseError('the answer is not application/json');
  }
  let body: unknown;
  try {
    body = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(await readBody(response, MAX_ANSWER_BYTES)));
  } catch {
    throw new ResponseError();
  }
  if (!isObject(body)) {
    throw new ResponseError();
  }
  return body;
}

export function createTransport(fetchResource: Fetch = fetch.bind(globalThis)): Transport {
  return {
    async send(method, path, options = {}) {
      const headers = new Headers(options.headers);
      if (method !== 'GET') {
        headers.set('X-Requested-With', 'XMLHttpRequest');
      }
      if (options.body !== undefined) {
        headers.set('Content-Type', 'application/json');
      }
      let response: Response;
      try {
        response = await fetchResource(path, {
          method,
          headers,
          ...(options.body !== undefined && { body: JSON.stringify(options.body) }),
          credentials: 'omit',
          cache: 'no-store',
          redirect: 'error',
          referrerPolicy: 'no-referrer',
          signal: AbortSignal.timeout(TIMEOUT_MS),
        });
      } catch (error) {
        throw new UnreachableError(error);
      }
      if (!response.ok) {
        throw await apiError(response);
      }
      return readAnswer(response);
    },
  };
}
