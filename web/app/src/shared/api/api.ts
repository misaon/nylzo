import {
  authorizationHeader,
  encodeBase64url,
  idToString,
  type KdfId,
  MAX_OBJECT_BYTES,
  parseChallenge,
  parseId,
  type Purpose,
  PURPOSE_MANAGE,
  PURPOSE_READ,
  PURPOSE_VERIFIER,
  type RequestKey,
  signRequest,
  transferSeconds,
  utf8,
  verifierHeader,
} from '@nylzo/core';

import { type MessageState, ResponseError, StoppedError } from './api-errors.ts';
import { type DownloadProgress, readObject } from './object-body.ts';
import {
  apiError,
  type ClientFile,
  clientFiles,
  instanceLimits,
  type InstanceLimits,
  integer,
  isObject,
  type Json,
  messageState,
  messageTimes,
  type MessageTimes,
  type Operator,
  operatorOf,
  readJson,
  servedPolicy,
  type ServedPolicy,
  text,
  timestamp,
} from './responses.ts';

export interface Transparency {
  readonly server: string;
  readonly operator: Operator | undefined;
  readonly limits: InstanceLimits;
  readonly rates: {
    readonly createPerHour: number;
    readonly createPerDay: number;
    readonly createBytesPerDay: number;
    readonly readPerHour: number;
    readonly abusePerHour: number;
    readonly coarseMultiplier: number;
    readonly globalDailyCreates: number;
    readonly powDifficulty: number;
  };
  readonly retention: {
    readonly tombstoneSeconds: number;
    readonly abuseReportSeconds: number;
    readonly dailyCounterSeconds: number;
  };
  readonly files: readonly ClientFile[];
}

export interface Instance {
  readonly fileCompression?: 'gzip';
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

export interface Metadata extends MessageTimes {
  readonly state: MessageState;
  readonly opensMax: number;
  readonly opensRemaining: number;
  readonly ttlSeconds: number;
  readonly passphraseRequired: boolean;
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
}

interface Claimed extends Envelope {
  readonly lease: string;
}

interface ClaimOptions {
  readonly verifierKey?: RequestKey | undefined;
  readonly onLease?: (lease: string) => void;
  readonly onProgress?: DownloadProgress;
  readonly proceed?: () => boolean;
}

export interface AbuseReport {
  readonly reason: string;
  readonly contact: string;
  readonly goodFaith: boolean;
}

export type ReportStatus = 'accepted' | 'limit';

export interface ApiOptions {
  readonly base?: string;
  readonly fetch?: typeof fetch;
}

export interface Api {
  instance: () => Promise<Instance>;
  transparency: () => Promise<Transparency>;
  create: (request: CreateRequest) => Promise<{ readonly expiresAt: string }>;
  metadata: (id: Uint8Array, readKey: RequestKey) => Promise<Metadata>;
  claim: (id: Uint8Array, readKey: RequestKey, options?: ClaimOptions) => Promise<Claimed>;
  blob: (id: Uint8Array, readKey: RequestKey, lease: string, onProgress?: DownloadProgress) => Promise<Envelope>;
  ack: (id: Uint8Array, readKey: RequestKey, lease: string) => Promise<void>;
  burn: (id: Uint8Array, readKey: RequestKey) => Promise<void>;
  cancel: (id: Uint8Array, manageKey: RequestKey) => Promise<void>;
  report: (id: Uint8Array, readKey: RequestKey, report: AbuseReport) => Promise<ReportStatus>;
  status: (id: Uint8Array, manageKey: RequestKey) => Promise<Status>;
}

interface SignedRequest {
  readonly method: 'GET' | 'POST';
  readonly path: string;
  readonly purpose: Purpose;
  readonly key: RequestKey;
  readonly verifier?: RequestKey | undefined;
  readonly headers?: Record<string, string>;
  readonly json?: Json;
  readonly proceed?: (() => boolean) | undefined;
}

const SHORT_TIMEOUT_MS = 30_000;
const TRANSFER_MARGIN_MS = 21_000;
export const TRANSFER_TIMEOUT_MS = transferSeconds(MAX_OBJECT_BYTES) * 1000 + TRANSFER_MARGIN_MS;

function createHeader(request: CreateRequest): string {
  const header = {
    id: idToString(request.id),
    id_token: request.idToken,
    read_key: encodeBase64url(request.readKey),
    ...(request.verifierKey && { verifier_key: encodeBase64url(request.verifierKey) }),
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

function leaseOf(response: Response): string {
  const lease = response.headers.get('X-Nylzo-Lease');
  if (lease === null) {
    throw new ResponseError('claim without a lease');
  }
  return lease;
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
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
      signal: AbortSignal.timeout(timeoutMs),
      ...requestInit,
      headers,
    });
  }

  async function challenge(): Promise<Uint8Array<ArrayBuffer>> {
    const response = await ensureOk(await call('/api/v1/challenge', { method: 'GET' }));
    return parseChallenge(text(await readJson(response), 'challenge'));
  }

  async function signedOnce(request: SignedRequest, timeoutMs: number): Promise<Response> {
    const fresh = await challenge();
    const headers: Record<string, string> = { ...request.headers };
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
    return call(request.path, { method: request.method, headers, ...(body !== undefined && { body }) }, timeoutMs);
  }

  async function signed(request: SignedRequest, timeoutMs = SHORT_TIMEOUT_MS): Promise<Response> {
    const response = await signedOnce(request, timeoutMs);
    if (response.status !== 404) {
      return ensureOk(response);
    }
    await response.body?.cancel();
    return ensureOk(await signedOnce(request, timeoutMs));
  }

  async function fetchJson(path: string): Promise<Json> {
    return readJson(await ensureOk(await call(path, { method: 'GET' })));
  }

  return {
    async instance() {
      const { limits, pow, create, file_compression: fileCompression } = await fetchJson('/api/v1/instance');
      if (!isObject(limits) || !isObject(pow) || !isObject(create) || typeof pow['required'] !== 'boolean') {
        throw new ResponseError();
      }
      return {
        ...(fileCompression === 'gzip' && { fileCompression }),
        limits: instanceLimits(limits),
        pow: {
          required: pow['required'],
          difficulty: integer(pow, 'difficulty'),
          ...(pow['challenge'] !== undefined && { challenge: text(pow, 'challenge') }),
        },
        create: { id: parseId(text(create, 'id')), idToken: text(create, 'id_token') },
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
          abusePerHour: integer(rates, 'abuse_per_hour'),
          coarseMultiplier: integer(rates, 'coarse_multiplier'),
          globalDailyCreates: integer(rates, 'global_daily_creates'),
          powDifficulty: integer(rates, 'pow_difficulty'),
        },
        retention: {
          tombstoneSeconds: integer(retention, 'tombstone_s'),
          abuseReportSeconds: integer(retention, 'abuse_report_s'),
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
      if (typeof json['passphrase_required'] !== 'boolean') {
        throw new ResponseError();
      }
      return {
        state: messageState(json),
        opensMax: integer(json, 'opens_max'),
        opensRemaining: integer(json, 'opens_remaining'),
        ttlSeconds: integer(json, 'ttl_s'),
        passphraseRequired: json['passphrase_required'],
        kdfId: integer(json, 'kdf_id'),
        size: integer(json, 'size'),
        ...messageTimes(json),
      };
    },

    async claim(id, readKey, claimOptions = {}) {
      const response = await signed(
        {
          method: 'POST',
          path: `${messagePath(id)}/claim`,
          purpose: PURPOSE_READ,
          key: readKey,
          verifier: claimOptions.verifierKey,
          proceed: claimOptions.proceed,
        },
        TRANSFER_TIMEOUT_MS,
      );
      const lease = leaseOf(response);
      claimOptions.onLease?.(lease);
      const policy = servedPolicy(response.headers.get('X-Nylzo-Policy'));
      return { lease, policy, object: await readObject(response, claimOptions.onProgress) };
    },

    async blob(id, readKey, lease, onProgress) {
      const response = await signed(
        {
          method: 'GET',
          path: `${messagePath(id)}/blob`,
          purpose: PURPOSE_READ,
          key: readKey,
          headers: { 'X-Nylzo-Lease': lease },
        },
        TRANSFER_TIMEOUT_MS,
      );
      const policy = servedPolicy(response.headers.get('X-Nylzo-Policy'));
      return { policy, object: await readObject(response, onProgress) };
    },

    async ack(id, readKey, lease) {
      await signed({
        method: 'POST',
        path: `${messagePath(id)}/ack`,
        purpose: PURPOSE_READ,
        key: readKey,
        headers: { 'X-Nylzo-Lease': lease },
      });
    },

    async burn(id, readKey) {
      await signed({ method: 'POST', path: `${messagePath(id)}/burn`, purpose: PURPOSE_READ, key: readKey });
    },

    async cancel(id, manageKey) {
      await signed({ method: 'POST', path: `${messagePath(id)}/cancel`, purpose: PURPOSE_MANAGE, key: manageKey });
    },

    async report(id, readKey, { reason, contact, goodFaith }) {
      const json = {
        id: idToString(id),
        ...(reason !== '' && { reason }),
        ...(contact !== '' && { contact }),
        ...(goodFaith && { good_faith: true }),
      };
      const response = await signed({
        method: 'POST',
        path: '/api/v1/abuse',
        purpose: PURPOSE_READ,
        key: readKey,
        json,
      });
      const status = text(await readJson(response), 'status');
      if (status !== 'accepted' && status !== 'limit') {
        throw new ResponseError('the server answered an unknown report status');
      }
      return status;
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
