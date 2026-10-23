import {
  PURPOSE_MANAGE,
  PURPOSE_READ,
  PURPOSE_VERIFIER,
  authorizationHeader,
  encodeBase64url,
  idToString,
  isKdfId,
  parseChallenge,
  parseId,
  signRequest,
  utf8,
  verifierHeader,
  type KdfId,
  type Purpose,
  type RequestKey,
} from '@nylzo/core';

export type MessageState = 'available' | 'opened' | 'expired' | 'cancelled' | 'deleted';

export type ApiErrorKind =
  | 'bad_request'
  | 'passphrase'
  | 'pow'
  | 'id_token'
  | 'not_found'
  | 'conflict'
  | 'gone'
  | 'length_required'
  | 'too_large'
  | 'rate_limited'
  | 'internal'
  | 'unavailable'
  | 'unexpected';

export class ApiError extends Error {
  override readonly name = 'ApiError';
  readonly status: number;
  readonly kind: ApiErrorKind;
  readonly retryAfter: number | undefined;
  readonly attemptsLeft: number | undefined;
  readonly state: MessageState | undefined;

  constructor(status: number, kind: ApiErrorKind, retryAfter?: number, attemptsLeft?: number, state?: MessageState) {
    super(`api ${status} ${kind}`);
    this.status = status;
    this.kind = kind;
    this.retryAfter = retryAfter;
    this.attemptsLeft = attemptsLeft;
    this.state = state;
  }
}

export class ResponseError extends Error {
  override readonly name = 'ResponseError';
  constructor(message = 'the server sent a malformed response') {
    super(message);
  }
}

export interface InstanceLimits {
  readonly maxFileBytes: number;
  readonly maxTextBytes: number;
  readonly maxObjectBytes: number;
  readonly maxTtlSeconds: number;
  readonly defaultTtlSeconds: number;
  readonly maxOpens: number;
  readonly defaultKdfId: KdfId;
}

export interface Instance {
  readonly limits: InstanceLimits;
  readonly pow: { readonly required: boolean; readonly difficulty: number; readonly challenge?: string };
  readonly create: { readonly id: Uint8Array<ArrayBuffer>; readonly idToken: string };
}

export interface CreateRequest {
  readonly id: Uint8Array;
  readonly idToken: string;
  readonly readKey: Uint8Array;
  readonly verifierKey?: Uint8Array;
  readonly manageKey: Uint8Array;
  readonly kdfId: KdfId;
  readonly opensMax: number;
  readonly ttlSeconds: number;
  readonly object: Uint8Array<ArrayBuffer>;
  readonly powToken?: string;
}

export interface Metadata {
  readonly state: MessageState;
  readonly opensMax: number;
  readonly opensRemaining: number;
  readonly ttlSeconds: number;
  readonly passphraseRequired: boolean;
  readonly kdfId: number;
  readonly size: number;
  readonly createdAt?: string;
  readonly expiresAt?: string;
  readonly firstOpenedAt?: string;
  readonly endedAt?: string;
}

export interface Status {
  readonly state: MessageState;
  readonly opensUsed: number;
  readonly opensMax: number;
  readonly failedPassphraseAttempts: number;
  readonly createdAt?: string;
  readonly expiresAt?: string;
  readonly firstOpenedAt?: string;
  readonly endedAt?: string;
}

export interface ServedPolicy {
  readonly opensMax: number;
  readonly ttlSeconds: number;
  readonly kdfId: number;
  readonly flags: number;
}

export interface Envelope {
  readonly policy: ServedPolicy;
  readonly object: Uint8Array<ArrayBuffer>;
}

export interface Claimed extends Envelope {
  readonly lease: string;
}

export type DownloadProgress = (received: number, total: number | undefined) => void;

export interface ClaimOptions {
  readonly verifierKey?: RequestKey | undefined;
  readonly onLease?: (lease: string) => void;
  readonly onProgress?: DownloadProgress;
}

export interface ApiOptions {
  readonly base?: string;
  readonly fetch?: typeof fetch;
}

interface SignedRequest {
  readonly method: 'GET' | 'POST';
  readonly path: string;
  readonly purpose: Purpose;
  readonly key: RequestKey;
  readonly verifier?: RequestKey | undefined;
  readonly headers?: Record<string, string>;
}

const STATES: readonly MessageState[] = ['available', 'opened', 'expired', 'cancelled', 'deleted'];
const KINDS: readonly ApiErrorKind[] = [
  'bad_request',
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
];

type Json = Record<string, unknown>;

function isObject(value: unknown): value is Json {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function integer(json: Json, key: string): number {
  const value = json[key];
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) {
    throw new ResponseError(`field ${key} is not a non-negative integer`);
  }
  return value;
}

function text(json: Json, key: string): string {
  const value = json[key];
  if (typeof value !== 'string') {
    throw new ResponseError(`field ${key} is not a string`);
  }
  return value;
}

function state(json: Json): MessageState {
  const value = json['state'];
  const found = STATES.find((s) => s === value);
  if (!found) {
    throw new ResponseError('unknown message state');
  }
  return found;
}

function times(json: Json): Pick<Metadata, 'createdAt' | 'expiresAt' | 'firstOpenedAt' | 'endedAt'> {
  const out: { createdAt?: string; expiresAt?: string; firstOpenedAt?: string; endedAt?: string } = {};
  for (const [wire, field] of [
    ['created_at', 'createdAt'],
    ['expires_at', 'expiresAt'],
    ['first_opened_at', 'firstOpenedAt'],
    ['ended_at', 'endedAt'],
  ] as const) {
    if (json[wire] !== undefined) {
      out[field] = text(json, wire);
    }
  }
  return out;
}

function retryAfterHeader(response: Response): number | undefined {
  const value = Number(response.headers.get('Retry-After'));
  return Number.isSafeInteger(value) && value >= 0 ? value : undefined;
}

async function readJson(response: Response): Promise<Json> {
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    throw new ResponseError();
  }
  if (!isObject(body)) {
    throw new ResponseError();
  }
  return body;
}

async function apiError(response: Response): Promise<ApiError> {
  let body: Json = {};
  try {
    body = await readJson(response);
  } catch {
    return new ApiError(response.status, 'unexpected', retryAfterHeader(response));
  }
  const kind = KINDS.find((k) => k === body['error']) ?? 'unexpected';
  const retryAfter = typeof body['retry_after'] === 'number' ? integer(body, 'retry_after') : retryAfterHeader(response);
  const attemptsLeft = typeof body['attempts_left'] === 'number' ? integer(body, 'attempts_left') : undefined;
  const ended = typeof body['state'] === 'string' ? state(body) : undefined;
  return new ApiError(response.status, kind, retryAfter, attemptsLeft, ended);
}

export function decodePolicyHeader(value: string | null): ServedPolicy {
  if (value === null || !/^[A-Za-z0-9_-]+$/.test(value)) {
    throw new ResponseError('missing or malformed X-Nylzo-Policy');
  }
  const binary = atob(value.replaceAll('-', '+').replaceAll('_', '/').padEnd(Math.ceil(value.length / 4) * 4, '='));
  const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
  if (encodeBase64url(bytes) !== value) {
    throw new ResponseError('X-Nylzo-Policy is not canonical base64url');
  }
  let json: unknown;
  try {
    json = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
  } catch {
    throw new ResponseError('X-Nylzo-Policy is not JSON');
  }
  if (!isObject(json)) {
    throw new ResponseError('X-Nylzo-Policy is not an object');
  }
  return {
    opensMax: integer(json, 'opens_max'),
    ttlSeconds: integer(json, 'ttl_s'),
    kdfId: integer(json, 'kdf_id'),
    flags: integer(json, 'flags'),
  };
}

export function encodeCreateHeader(request: Omit<CreateRequest, 'object' | 'powToken'>, size: number): string {
  const header = {
    id: idToString(request.id),
    id_token: request.idToken,
    read_key: encodeBase64url(request.readKey),
    ...(request.verifierKey ? { verifier_key: encodeBase64url(request.verifierKey) } : {}),
    manage_key: encodeBase64url(request.manageKey),
    kdf_id: request.kdfId,
    opens_max: request.opensMax,
    ttl_s: request.ttlSeconds,
    size,
  };
  return encodeBase64url(utf8(JSON.stringify(header)));
}

async function readBody(response: Response, onProgress?: DownloadProgress): Promise<Uint8Array<ArrayBuffer>> {
  const declared = Number(response.headers.get('Content-Length'));
  const total = Number.isSafeInteger(declared) && declared > 0 ? declared : undefined;
  if (!response.body) {
    return new Uint8Array(await response.arrayBuffer());
  }
  const chunks: Uint8Array[] = [];
  let received = 0;
  const reader = response.body.getReader();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    chunks.push(value);
    received += value.length;
    onProgress?.(received, total);
  }
  const out = new Uint8Array(received);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}

export function createApi(options: ApiOptions = {}) {
  const base = options.base ?? '';
  const fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);

  function call(path: string, init: RequestInit): Promise<Response> {
    return fetchImpl(`${base}${path}`, {
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
      ...init,
    });
  }

  async function challenge(): Promise<Uint8Array<ArrayBuffer>> {
    const response = await call('/api/v1/challenge', { method: 'GET' });
    if (!response.ok) {
      throw await apiError(response);
    }
    return parseChallenge(text(await readJson(response), 'challenge'));
  }

  async function signedOnce(request: SignedRequest): Promise<Response> {
    const fresh = await challenge();
    const headers: Record<string, string> = { ...request.headers };
    const auth = await signRequest(request.key, request.purpose, request.method, request.path, fresh);
    headers['Authorization'] = authorizationHeader(auth);
    if (request.verifier) {
      const verifier = await signRequest(request.verifier, PURPOSE_VERIFIER, request.method, request.path, fresh);
      headers['X-Nylzo-Verifier'] = verifierHeader(verifier);
    }
    return call(request.path, { method: request.method, headers });
  }

  async function signed(request: SignedRequest): Promise<Response> {
    const response = await signedOnce(request);
    if (response.status !== 404) {
      return response;
    }
    await response.body?.cancel();
    return signedOnce(request);
  }

  async function ensureOk(response: Response): Promise<Response> {
    if (!response.ok) {
      throw await apiError(response);
    }
    return response;
  }

  const messagePath = (id: Uint8Array) => `/api/v1/messages/${idToString(id)}`;

  return {
    async instance(): Promise<Instance> {
      const json = await readJson(await ensureOk(await call('/api/v1/instance', { method: 'GET' })));
      const limits = json['limits'];
      const pow = json['pow'];
      const create = json['create'];
      if (!isObject(limits) || !isObject(pow) || !isObject(create) || typeof pow['required'] !== 'boolean') {
        throw new ResponseError();
      }
      const defaultKdfId = integer(limits, 'default_kdf_id');
      if (!isKdfId(defaultKdfId)) {
        throw new ResponseError('unknown default kdf profile');
      }
      return {
        limits: {
          maxFileBytes: integer(limits, 'max_file_bytes'),
          maxTextBytes: integer(limits, 'max_text_bytes'),
          maxObjectBytes: integer(limits, 'max_object_bytes'),
          maxTtlSeconds: integer(limits, 'max_ttl_s'),
          defaultTtlSeconds: integer(limits, 'default_ttl_s'),
          maxOpens: integer(limits, 'max_opens'),
          defaultKdfId,
        },
        pow: {
          required: pow['required'],
          difficulty: integer(pow, 'difficulty'),
          ...(pow['challenge'] === undefined ? {} : { challenge: text(pow, 'challenge') }),
        },
        create: { id: parseId(text(create, 'id')), idToken: text(create, 'id_token') },
      };
    },

    async create(request: CreateRequest): Promise<{ readonly expiresAt: string }> {
      const headers: Record<string, string> = {
        'Content-Type': 'application/octet-stream',
        'X-Nylzo-Create': encodeCreateHeader(request, request.object.length),
      };
      if (request.powToken !== undefined) {
        headers['X-Nylzo-Pow'] = request.powToken;
      }
      const response = await ensureOk(await call('/api/v1/messages', { method: 'POST', headers, body: request.object }));
      return { expiresAt: text(await readJson(response), 'expires_at') };
    },

    async metadata(id: Uint8Array, readKey: RequestKey): Promise<Metadata> {
      const response = await ensureOk(await signed({ method: 'GET', path: messagePath(id), purpose: PURPOSE_READ, key: readKey }));
      const json = await readJson(response);
      if (typeof json['passphrase_required'] !== 'boolean') {
        throw new ResponseError();
      }
      return {
        state: state(json),
        opensMax: integer(json, 'opens_max'),
        opensRemaining: integer(json, 'opens_remaining'),
        ttlSeconds: integer(json, 'ttl_s'),
        passphraseRequired: json['passphrase_required'],
        kdfId: integer(json, 'kdf_id'),
        size: integer(json, 'size'),
        ...times(json),
      };
    },

    async claim(id: Uint8Array, readKey: RequestKey, options: ClaimOptions = {}): Promise<Claimed> {
      const response = await ensureOk(
        await signed({ method: 'POST', path: `${messagePath(id)}/claim`, purpose: PURPOSE_READ, key: readKey, verifier: options.verifierKey }),
      );
      const lease = response.headers.get('X-Nylzo-Lease');
      if (lease === null) {
        throw new ResponseError('claim without a lease');
      }
      options.onLease?.(lease);
      const policy = decodePolicyHeader(response.headers.get('X-Nylzo-Policy'));
      return { lease, policy, object: await readBody(response, options.onProgress) };
    },

    async blob(id: Uint8Array, readKey: RequestKey, lease: string, onProgress?: DownloadProgress): Promise<Envelope> {
      const response = await ensureOk(
        await signed({ method: 'GET', path: `${messagePath(id)}/blob`, purpose: PURPOSE_READ, key: readKey, headers: { 'X-Nylzo-Lease': lease } }),
      );
      const policy = decodePolicyHeader(response.headers.get('X-Nylzo-Policy'));
      return { policy, object: await readBody(response, onProgress) };
    },

    async ack(id: Uint8Array, readKey: RequestKey, lease: string): Promise<void> {
      await ensureOk(
        await signed({ method: 'POST', path: `${messagePath(id)}/ack`, purpose: PURPOSE_READ, key: readKey, headers: { 'X-Nylzo-Lease': lease } }),
      );
    },

    async status(id: Uint8Array, manageKey: RequestKey): Promise<Status> {
      const response = await ensureOk(await signed({ method: 'GET', path: `${messagePath(id)}/status`, purpose: PURPOSE_MANAGE, key: manageKey }));
      const json = await readJson(response);
      return {
        state: state(json),
        opensUsed: integer(json, 'opens_used'),
        opensMax: integer(json, 'opens_max'),
        failedPassphraseAttempts: integer(json, 'failed_passphrase_attempts'),
        ...times(json),
      };
    },
  };
}

export type Api = ReturnType<typeof createApi>;
