import { encodeBase64url, idToString, type KdfId, type RequestKey } from '@nylzo/core';

import { ApiError, type Api, type CreateRequest, type Instance, type MessageState, type Metadata } from '../src/lib/api.ts';

interface Stored {
  readonly request: CreateRequest;
  state: MessageState;
  opensRemaining: number;
  failures: number;
  leases: Set<string>;
}

export interface FakeOptions {
  pow?: { required: boolean; difficulty: number };
  failCreate?: ApiError[];
  servedKdfId?: number;
  servedOpensMax?: number;
  servedFlags?: number;
  tombstone?: boolean;
  metadataKdfId?: number;
  tamperObject?: boolean;
  expiresInSeconds?: number;
  lockAfter?: number;
}

export interface FakeApi extends Api {
  readonly calls: string[];
  readonly challenges: string[];
  readonly stored: Map<string, Stored>;
  readonly issuedIds: Uint8Array<ArrayBuffer>[];
}

const equal = (a: Uint8Array | undefined, b: Uint8Array | undefined) =>
  a !== undefined && b !== undefined && a.length === b.length && a.every((v, i) => v === b[i]);

export function fakeApi(options: FakeOptions = {}): FakeApi {
  const calls: string[] = [];
  const challenges: string[] = [];
  const stored = new Map<string, Stored>();
  const issuedIds: Uint8Array<ArrayBuffer>[] = [];
  const failCreate = [...(options.failCreate ?? [])];
  const expiresAt = new Date(Date.now() + (options.expiresInSeconds ?? 3600) * 1000).toISOString();

  function find(id: Uint8Array, readKey: RequestKey): Stored {
    const message = stored.get(idToString(id));
    if (!message || !equal(message.request.readKey, readKey.publicKey)) {
      throw new ApiError(404, 'not_found');
    }
    return message;
  }

  function envelope(message: Stored) {
    const object = new Uint8Array(message.request.object);
    if (options.tamperObject) {
      object[object.length - 1] = (object[object.length - 1] ?? 0) ^ 1;
    }
    return {
      policy: {
        opensMax: options.servedOpensMax ?? message.request.opensMax,
        ttlSeconds: message.request.ttlSeconds,
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
        opensMax: message.request.opensMax,
        opensRemaining: message.opensRemaining,
        ttlSeconds: message.request.ttlSeconds,
        passphraseRequired: message.request.kdfId !== 0,
        kdfId: options.metadataKdfId ?? message.request.kdfId,
        size: message.request.object.length,
        createdAt: new Date().toISOString(),
        expiresAt,
      };
    },
    async claim(id, readKey, claimOptions = {}) {
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
    async status() {
      throw new Error('not used');
    },
  };
  return Object.assign(api, { calls, challenges, stored, issuedIds });
}
