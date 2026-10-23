import { encodeBase64url, idToString, type KdfId, type RequestKey } from '@nylzo/core';

import { ApiError, StoppedError, type Api, type CreateRequest, type Instance, type MessageState, type Metadata, type Status } from '../src/lib/api.ts';

interface Stored {
  readonly request: CreateRequest;
  state: MessageState;
  opensRemaining: number;
  failures: number;
  leases: Set<string>;
  endedAt?: string;
}

export interface Report {
  readonly id: string;
  readonly signedByReader: boolean;
  readonly reason: string;
}

export interface FakeOptions {
  pow?: { required: boolean; difficulty: number };
  failCreate?: ApiError[];
  servedKdfId?: number;
  servedOpensMax?: number;
  servedTtlSeconds?: number;
  servedFlags?: number;
  tombstone?: boolean;
  metadataKdfId?: number;
  metadataOpensMax?: number;
  metadataTtlSeconds?: number;
  tamperObject?: boolean;
  expiresInSeconds?: number;
  lockAfter?: number;
  failStatus?: ApiError[];
  failCancel?: ApiError[];
  failBurn?: ApiError[];
  failReport?: ApiError[];
}

export interface FakeApi extends Api {
  readonly calls: string[];
  readonly challenges: string[];
  readonly stored: Map<string, Stored>;
  readonly issuedIds: Uint8Array<ArrayBuffer>[];
  readonly reports: Report[];
}

const equal = (a: Uint8Array | undefined, b: Uint8Array | undefined) =>
  a !== undefined && b !== undefined && a.length === b.length && a.every((v, i) => v === b[i]);

export function fakeApi(options: FakeOptions = {}): FakeApi {
  const calls: string[] = [];
  const challenges: string[] = [];
  const stored = new Map<string, Stored>();
  const issuedIds: Uint8Array<ArrayBuffer>[] = [];
  const failCreate = [...(options.failCreate ?? [])];
  const failStatus = [...(options.failStatus ?? [])];
  const failCancel = [...(options.failCancel ?? [])];
  const failBurn = [...(options.failBurn ?? [])];
  const failReport = [...(options.failReport ?? [])];
  const reports: Report[] = [];
  const expiresAt = new Date(Date.now() + (options.expiresInSeconds ?? 3600) * 1000).toISOString();

  function find(id: Uint8Array, readKey: RequestKey): Stored {
    const message = stored.get(idToString(id));
    if (!message || !equal(message.request.readKey, readKey.publicKey)) {
      throw new ApiError(404, 'not_found');
    }
    return message;
  }

  function managed(id: Uint8Array, manageKey: RequestKey): Stored {
    const message = stored.get(idToString(id));
    if (!message || !equal(message.request.manageKey, manageKey.publicKey)) {
      throw new ApiError(404, 'not_found');
    }
    return message;
  }

  function end(message: Stored, state: MessageState): void {
    if (message.state !== 'available' && !(message.state === 'opened' && message.leases.size > 0)) {
      throw new ApiError(410, 'gone', undefined, undefined, message.state);
    }
    message.state = state;
    message.leases.clear();
    message.endedAt = '2026-10-23T11:00:00Z';
  }

  function envelope(message: Stored) {
    const object = new Uint8Array(message.request.object);
    if (options.tamperObject) {
      object[object.length - 1] = (object[object.length - 1] ?? 0) ^ 1;
    }
    return {
      policy: {
        opensMax: options.servedOpensMax ?? message.request.opensMax,
        ttlSeconds: options.servedTtlSeconds ?? message.request.ttlSeconds,
        kdfId: options.servedKdfId ?? message.request.kdfId,
        flags: options.servedFlags ?? 0,
      },
      object,
    };
  }

  const api: Api = {
    async instance(): Promise<Instance> {
      calls.push('instance');
      const id = crypto.getRandomValues(new Uint8Array(16));
      issuedIds.push(id);
      const challenge = encodeBase64url(new Uint8Array(36).fill(challenges.length + 1));
      challenges.push(challenge);
      return {
        limits: {
          maxFileBytes: 5242880,
          maxTextBytes: 102400,
          maxObjectBytes: 5373983,
          maxTtlSeconds: 604800,
          defaultTtlSeconds: 86400,
          maxOpens: 10,
          defaultKdfId: 1 as KdfId,
        },
        pow: options.pow?.required ? { ...options.pow, challenge } : { required: false, difficulty: 4 },
        create: { id, idToken: 'token' },
      };
    },
    async create(request) {
      calls.push('create');
      const failure = failCreate.shift();
      if (failure) {
        throw failure;
      }
      stored.set(idToString(request.id), { request, state: 'available', opensRemaining: request.opensMax, failures: 0, leases: new Set() });
      return { expiresAt };
    },
    async metadata(id, readKey): Promise<Metadata> {
      calls.push('metadata');
      const message = find(id, readKey);
      if (options.tombstone) {
        return {
          state: message.state,
          opensMax: 0,
          opensRemaining: 0,
          ttlSeconds: 0,
          passphraseRequired: false,
          kdfId: 0,
          size: 0,
          firstOpenedAt: '2026-10-23T10:00:00Z',
          endedAt: '2026-10-23T10:05:00Z',
        };
      }
      return {
        state: message.state,
        opensMax: options.metadataOpensMax ?? message.request.opensMax,
        opensRemaining: message.opensRemaining,
        ttlSeconds: options.metadataTtlSeconds ?? message.request.ttlSeconds,
        passphraseRequired: message.request.kdfId !== 0,
        kdfId: options.metadataKdfId ?? message.request.kdfId,
        size: message.request.object.length,
        createdAt: new Date().toISOString(),
        expiresAt,
      };
    },
    async claim(id, readKey, claimOptions = {}) {
      if (claimOptions.proceed?.() === false) {
        throw new StoppedError();
      }
      calls.push(claimOptions.verifierKey ? 'claim+verifier' : 'claim');
      const message = find(id, readKey);
      if (message.state !== 'available') {
        throw new ApiError(410, 'gone', undefined, undefined, message.state);
      }
      if (message.request.kdfId !== 0 && !equal(message.request.verifierKey, claimOptions.verifierKey?.publicKey)) {
        message.failures++;
        const locked = message.failures >= (options.lockAfter ?? 100);
        throw new ApiError(403, 'passphrase', locked ? 3600 : 0, locked ? 0 : Math.max(0, 5 - message.failures));
      }
      message.opensRemaining--;
      if (message.opensRemaining === 0) {
        message.state = 'opened';
      }
      const lease = `lease-${message.leases.size + 1}`;
      message.leases.add(lease);
      claimOptions.onLease?.(lease);
      const served = envelope(message);
      claimOptions.onProgress?.(served.object.length, served.object.length);
      return { lease, ...served };
    },
    async blob(id, readKey, lease) {
      calls.push('blob');
      const message = find(id, readKey);
      if (!message.leases.has(lease)) {
        throw new ApiError(410, 'gone', undefined, undefined, message.state);
      }
      return envelope(message);
    },
    async ack(id, readKey, lease) {
      calls.push('ack');
      find(id, readKey).leases.delete(lease);
    },
    async burn(id, readKey) {
      calls.push('burn');
      const failure = failBurn.shift();
      if (failure) {
        throw failure;
      }
      end(find(id, readKey), 'deleted');
    },
    async cancel(id, manageKey) {
      calls.push('cancel');
      const failure = failCancel.shift();
      if (failure) {
        throw failure;
      }
      end(managed(id, manageKey), 'cancelled');
    },
    async report(id, readKey, reason) {
      calls.push('report');
      const failure = failReport.shift();
      if (failure) {
        throw failure;
      }
      const message = stored.get(idToString(id));
      reports.push({ id: idToString(id), signedByReader: equal(message?.request.readKey, readKey.publicKey), reason });
    },
    async status(id, manageKey): Promise<Status> {
      calls.push('status');
      const failure = failStatus.shift();
      if (failure) {
        throw failure;
      }
      const message = managed(id, manageKey);
      if (message.endedAt) {
        return { state: message.state, opensUsed: 0, opensMax: 0, failedPassphraseAttempts: 0, endedAt: message.endedAt };
      }
      return {
        state: message.state,
        opensUsed: message.request.opensMax - message.opensRemaining,
        opensMax: message.request.opensMax,
        failedPassphraseAttempts: message.failures,
        createdAt: '2026-10-23T10:00:00Z',
        expiresAt,
      };
    },
  };
  return Object.assign(api, { calls, challenges, stored, issuedIds, reports });
}
