import {
  authorizationHeader,
  decodeBase64url,
  encodeBase64url,
  idToString,
  MAX_OBJECT_BYTES,
  MAX_POW_DIFFICULTY,
  parseChallenge,
  parseId,
  parseWrappedShare,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  PURPOSE_VERIFIER,
  signRequest,
  transferSeconds,
  utf8,
  verifierHeader,
} from '@nylzo/core';
import type { KdfId, Purpose, RequestKey, ShareKey } from '@nylzo/core';

import { ResponseError, StoppedError } from './api-errors.ts';
import type { MessageState } from './api-errors.ts';
import { readObject } from './object-body.ts';
import type { DownloadProgress } from './object-body.ts';
import {
  apiError,
  clientFiles,
  instanceLimits,
  integer,
  isObject,
  messageState,
  messageTimes,
  operatorOf,
  readJson,
  servedPolicy,
  text,
  timestamp,
} from './responses.ts';
import type { ClientFile, InstanceLimits, Json, MessageTimes, Operator, ServedPolicy } from './responses.ts';

export interface Transparency {
  readonly server: string;
  readonly operator: Operator | undefined;
  readonly limits: InstanceLimits;
  readonly rates: {
    readonly createPerHour: number;
    readonly createPerDay: number;
    readonly createBytesPerDay: number;
    readonly readPerHour: number;
    readonly coarseMultiplier: number;
    readonly globalDailyCreates: number;
    readonly powDifficulty: number;
  };
  readonly retention: {
    readonly tombstoneSeconds: number;
    readonly abuseReportSeconds?: number;
    readonly dailyCounterSeconds: number;
  };
  readonly files: readonly ClientFile[];
}

// An id the instance issued, with the expiry share still wrapped to the client's one-time key.
export interface Issued {
  readonly id: Uint8Array<ArrayBuffer>;
  readonly idToken: string;
  readonly epoch: number;
  readonly share: Uint8Array<ArrayBuffer>;
}

export interface Instance {
  readonly limits: InstanceLimits;
  readonly pow: { readonly required: boolean; readonly difficulty: number; readonly challenge?: string };
  readonly create?: Issued | undefined;
}

interface Creation {
  readonly ttlSeconds: number;
  readonly shareKey: Uint8Array;
}

export interface CreateRequest {
  readonly id: Uint8Array;
  readonly idToken: string;
  readonly epoch: number;
  readonly readKey: Uint8Array;
  readonly verifierKey: Uint8Array;
  readonly manageKey: Uint8Array;
  readonly kdfId: KdfId;
  readonly opensMax: number;
  readonly ttlSeconds: number;
  readonly object: Uint8Array<ArrayBuffer>;
  readonly powToken?: string;
}

export interface Metadata extends MessageTimes {
  readonly state: MessageState;
  readonly opensMax: number;
  readonly opensRemaining: number;
  readonly ttlSeconds: number;
  readonly kdfId: number;
  readonly size: number;
}

export interface Status extends MessageTimes {
  readonly state: MessageState;
  readonly opensUsed: number;
  readonly opensMax: number;
  readonly failedPassphraseAttempts: number;
}

export interface Envelope {
  readonly policy: ServedPolicy;
  readonly object: Uint8Array<ArrayBuffer>;
  readonly share?: Uint8Array<ArrayBuffer> | undefined;
  readonly shareKey: ShareKey;
}

// Each attempt draws its own share key, so no key ever serves two requests (Protocol §5.6).
type ShareKeys = () => Promise<ShareKey>;

interface Claimed extends Envelope {
  readonly lease: string;
}

interface ClaimOptions {
  readonly verifierKey: RequestKey;
  readonly shareKeys: ShareKeys;
  readonly onLease?: (lease: string) => void;
  readonly onProgress?: DownloadProgress;
  readonly proceed?: () => boolean;
}

export interface ApiOptions {
  readonly base?: string;
  readonly fetch?: typeof fetch;
}

export interface Api {
  instance: (creation?: Creation) => Promise<Instance>;
  transparency: () => Promise<Transparency>;
  create: (request: CreateRequest) => Promise<{ readonly expiresAt: string }>;
  metadata: (id: Uint8Array, readKey: RequestKey) => Promise<Metadata>;
  claim: (id: Uint8Array, readKey: RequestKey, options: ClaimOptions) => Promise<Claimed>;
  blob: (
    id: Uint8Array,
    readKey: RequestKey,
    lease: string,
    shareKeys: ShareKeys,
    onProgress?: DownloadProgress,
  ) => Promise<Envelope>;
  challenge: () => Promise<Uint8Array<ArrayBuffer>>;
  ack: (id: Uint8Array, readKey: RequestKey, lease: string, challenge?: Uint8Array<ArrayBuffer>) => Promise<void>;
  burn: (id: Uint8Array, readKey: RequestKey) => Promise<void>;
  cancel: (id: Uint8Array, manageKey: RequestKey) => Promise<void>;
  status: (id: Uint8Array, manageKey: RequestKey) => Promise<Status>;
}

interface SignedRequest {
  readonly method: 'GET' | 'POST';
  readonly path: string;
  readonly purpose: Purpose;
  readonly key: RequestKey;
  readonly verifier?: RequestKey | undefined;
  readonly headers?: Readonly<Record<string, string>>;
  readonly attemptHeaders?: () => Promise<Readonly<Record<string, string>>>;
  readonly json?: Json;
  readonly proceed?: (() => boolean) | undefined;
  readonly keepalive?: boolean;
  readonly challenge?: Uint8Array<ArrayBuffer> | undefined;
}

const SHORT_TIMEOUT_MS = 30_000;
const LEASE_LENGTH = 32;
const TRANSFER_MARGIN_MS = 21_000;
export const TRANSFER_TIMEOUT_MS = transferSeconds(MAX_OBJECT_BYTES) * 1000 + TRANSFER_MARGIN_MS;

function createHeader(request: CreateRequest): string {
  const header = {
    id: idToString(request.id),
    id_token: request.idToken,
    epoch: request.epoch,
    read_key: encodeBase64url(request.readKey),
    verifier_key: encodeBase64url(request.verifierKey),
    manage_key: encodeBase64url(request.manageKey),
    kdf_id: request.kdfId,
    opens_max: request.opensMax,
    ttl_s: request.ttlSeconds,
    size: request.object.length,
  };
  return encodeBase64url(utf8(JSON.stringify(header)));
}

function messagePath(id: Uint8Array): string {
  return `/api/v1/messages/${idToString(id)}`;
}

async function ensureOk(response: Response): Promise<Response> {
  if (!response.ok) {
    throw await apiError(response);
  }
  return response;
}

function shareOf(response: Response): Uint8Array<ArrayBuffer> | undefined {
  const share = response.headers.get('X-Nylzo-Share');
  if (share === null) {
    return undefined;
  }
  try {
    return parseWrappedShare(share);
  } catch {
    throw new ResponseError('a malformed share');
  }
}

function issuedOf(create: Json): Issued {
  return {
    id: parseId(text(create, 'id')),
    idToken: text(create, 'id_token'),
    epoch: integer(create, 'epoch'),
    share: parseWrappedShare(text(create, 'share')),
  };
}

function leaseOf(response: Response): string {
  const lease = response.headers.get('X-Nylzo-Lease') ?? '';
  let length = 0;
  try {
    length = decodeBase64url(lease).length;
  } catch {}
  if (length !== LEASE_LENGTH) {
    throw new ResponseError('claim without a canonical lease');
  }
  return lease;
}

function sentShareKey(shareKeys: ShareKeys) {
  let current: ShareKey | undefined;
  return {
    headers: async () => {
      current = await shareKeys();
      return { 'X-Nylzo-Share-Key': encodeBase64url(current.publicKey) };
    },
    last: (): ShareKey => {
      if (!current) {
        throw new ResponseError('no share key was sent');
      }
      return current;
    },
  };
}

export function createApi(options: ApiOptions = {}): Api {
  const base = options.base ?? '';
  const fetchResource = options.fetch ?? fetch.bind(globalThis);

  async function call(path: string, requestInit: RequestInit, timeoutMs = SHORT_TIMEOUT_MS): Promise<Response> {
    const headers = new Headers(requestInit.headers);
    const method = requestInit.method?.toUpperCase() ?? 'GET';
    if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      headers.set('X-Requested-With', 'XMLHttpRequest');
    }
    return fetchResource(`${base}${path}`, {
      ...requestInit,
      headers,
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
      signal: AbortSignal.timeout(timeoutMs),
    });
  }

  async function challenge(): Promise<Uint8Array<ArrayBuffer>> {
    const response = await ensureOk(await call('/api/v1/challenge', { method: 'GET' }));
    return parseChallenge(text(await readJson(response), 'challenge'));
  }

  async function signedOnce(request: SignedRequest, timeoutMs: number): Promise<Response> {
    const fresh = request.challenge ?? (await challenge());
    const headers: Record<string, string> = { ...request.headers, ...(await request.attemptHeaders?.()) };
    const body = request.json === undefined ? undefined : utf8(JSON.stringify(request.json));
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
    }
    const signature = await signRequest(request.key, request.purpose, request.method, request.path, fresh, body);
    headers['Authorization'] = authorizationHeader(signature);
    if (request.verifier) {
      const verifier = await signRequest(request.verifier, PURPOSE_VERIFIER, request.method, request.path, fresh);
      headers['X-Nylzo-Verifier'] = verifierHeader(verifier);
    }
    if (request.proceed?.() === false) {
      throw new StoppedError();
    }
    return call(
      request.path,
      { method: request.method, headers, keepalive: request.keepalive === true, ...(body !== undefined && { body }) },
      timeoutMs,
    );
  }

  async function signed(request: SignedRequest, timeoutMs = SHORT_TIMEOUT_MS): Promise<Response> {
    const response = await signedOnce(request, timeoutMs);
    if (response.status !== 404) {
      return ensureOk(response);
    }
    await response.body?.cancel();
    return ensureOk(await signedOnce({ ...request, challenge: undefined }, timeoutMs));
  }

  async function fetchJson(path: string, headers?: Readonly<Record<string, string>>): Promise<Json> {
    return readJson(await ensureOk(await call(path, { method: 'GET', ...(headers && { headers }) })));
  }

  return {
    async instance(creation) {
      const headers =
        creation === undefined
          ? undefined
          : { 'X-Nylzo-Ttl': String(creation.ttlSeconds), 'X-Nylzo-Share-Key': encodeBase64url(creation.shareKey) };
      const { limits, pow, create } = await fetchJson('/api/v1/instance', headers);
      if (!isObject(limits) || !isObject(pow) || typeof pow['required'] !== 'boolean') {
        throw new ResponseError();
      }
      const difficulty = integer(pow, 'difficulty');
      if (difficulty > MAX_POW_DIFFICULTY || (creation !== undefined && !isObject(create))) {
        throw new ResponseError('the instance asks for more than the spec allows');
      }
      return {
        limits: instanceLimits(limits),
        pow: {
          required: pow['required'],
          difficulty,
          ...(pow['challenge'] !== undefined && { challenge: text(pow, 'challenge') }),
        },
        ...(isObject(create) && creation !== undefined && { create: issuedOf(create) }),
      };
    },

    async transparency() {
      const [report, manifest] = await Promise.all([
        fetchJson('/.well-known/nylzo/transparency.json'),
        fetchJson('/.well-known/nylzo/client.json'),
      ]);
      const { limits, rates, retention } = report;
      if (
        !isObject(limits) ||
        !isObject(rates) ||
        !isObject(retention) ||
        text(manifest, 'server') !== text(report, 'server')
      ) {
        throw new ResponseError();
      }
      return {
        server: text(report, 'server'),
        operator: operatorOf(report['operator']),
        limits: instanceLimits(limits),
        rates: {
          createPerHour: integer(rates, 'create_per_hour'),
          createPerDay: integer(rates, 'create_per_day'),
          createBytesPerDay: integer(rates, 'create_bytes_per_day'),
          readPerHour: integer(rates, 'read_per_hour'),
          coarseMultiplier: integer(rates, 'coarse_multiplier'),
          globalDailyCreates: integer(rates, 'global_daily_creates'),
          powDifficulty: integer(rates, 'pow_difficulty'),
        },
        retention: {
          tombstoneSeconds: integer(retention, 'tombstone_s'),
          ...(retention['abuse_report_s'] !== undefined && {
            abuseReportSeconds: integer(retention, 'abuse_report_s'),
          }),
          dailyCounterSeconds: integer(retention, 'daily_counter_s'),
        },
        files: clientFiles(manifest),
      };
    },

    async create(request) {
      const headers: Record<string, string> = {
        'Content-Type': 'application/octet-stream',
        'X-Nylzo-Create': createHeader(request),
      };
      if (request.powToken !== undefined) {
        headers['X-Nylzo-Pow'] = request.powToken;
      }
      const response = await ensureOk(
        await call('/api/v1/messages', { method: 'POST', headers, body: request.object }, TRANSFER_TIMEOUT_MS),
      );
      return { expiresAt: timestamp(await readJson(response), 'expires_at') };
    },

    async metadata(id, readKey) {
      const response = await signed({ method: 'GET', path: messagePath(id), purpose: PURPOSE_READ, key: readKey });
      const json = await readJson(response);
      return {
        state: messageState(json),
        opensMax: integer(json, 'opens_max'),
        opensRemaining: integer(json, 'opens_remaining'),
        ttlSeconds: integer(json, 'ttl_s'),
        kdfId: integer(json, 'kdf_id'),
        size: integer(json, 'size'),
        ...messageTimes(json),
      };
    },

    async claim(id, readKey, claimOptions) {
      const sent = sentShareKey(claimOptions.shareKeys);
      const response = await signed(
        {
          method: 'POST',
          path: `${messagePath(id)}/claim`,
          purpose: PURPOSE_READ,
          key: readKey,
          verifier: claimOptions.verifierKey,
          attemptHeaders: sent.headers,
          proceed: claimOptions.proceed,
        },
        TRANSFER_TIMEOUT_MS,
      );
      const lease = leaseOf(response);
      claimOptions.onLease?.(lease);
      const policy = servedPolicy(response.headers.get('X-Nylzo-Policy'));
      const share = shareOf(response);
      const object = await readObject(response, claimOptions.onProgress);
      return { lease, policy, share, shareKey: sent.last(), object };
    },

    async blob(id, readKey, lease, shareKeys, onProgress) {
      const sent = sentShareKey(shareKeys);
      const response = await signed(
        {
          method: 'GET',
          path: `${messagePath(id)}/blob`,
          purpose: PURPOSE_READ,
          key: readKey,
          headers: { 'X-Nylzo-Lease': lease },
          attemptHeaders: sent.headers,
        },
        TRANSFER_TIMEOUT_MS,
      );
      const policy = servedPolicy(response.headers.get('X-Nylzo-Policy'));
      const share = shareOf(response);
      return { policy, share, shareKey: sent.last(), object: await readObject(response, onProgress) };
    },

    challenge,

    // A challenge fetched before the message shows lets the ack leave as one keepalive request, even as the page closes.
    async ack(id, readKey, lease, prepared) {
      await signed({
        challenge: prepared,
        method: 'POST',
        path: `${messagePath(id)}/ack`,
        purpose: PURPOSE_READ,
        key: readKey,
        headers: { 'X-Nylzo-Lease': lease },
        keepalive: true,
      });
    },

    async burn(id, readKey) {
      await signed({ method: 'POST', path: `${messagePath(id)}/burn`, purpose: PURPOSE_READ, key: readKey });
    },

    async cancel(id, manageKey) {
      await signed({ method: 'POST', path: `${messagePath(id)}/cancel`, purpose: PURPOSE_MANAGE, key: manageKey });
    },

    async status(id, manageKey) {
      const response = await signed({
        method: 'GET',
        path: `${messagePath(id)}/status`,
        purpose: PURPOSE_MANAGE,
        key: manageKey,
      });
      const json = await readJson(response);
      return {
        state: messageState(json),
        opensUsed: integer(json, 'opens_used'),
        opensMax: integer(json, 'opens_max'),
        failedPassphraseAttempts: integer(json, 'failed_passphrase_attempts'),
        ...messageTimes(json),
      };
    },
  };
}
