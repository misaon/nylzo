import { encodeBase64url, idToString, type RequestKey } from '@nylzo/core';

import type { AbuseReport, Api, CreateRequest, Instance, Metadata, Status } from './api.ts';

import { ApiError, type MessageState, StoppedError } from './api-errors.ts';

interface Stored {
  readonly request: CreateRequest;
  state: MessageState;
  opensRemaining: number;
  failures: number;
  leases: Set<string>;
  endedAt?: string;
}

interface Report extends AbuseReport {
  readonly id: string;
  readonly signedByReader: boolean;
}

const REPORTS_PER_MESSAGE = 3;

export interface FakeOptions {
  fileCompression?: boolean;
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

export function abuseReport(reason: string, contact = ''): AbuseReport {
  return { reason, contact, goodFaith: true };
}

function sameBytes(first: Uint8Array | undefined, second: Uint8Array | undefined): boolean {
  return (
    first !== undefined && second?.length === first.length && first.every((value, index) => value === second[index])
  );
}

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
    if (!message || !sameBytes(message.request.readKey, readKey.publicKey)) {
      throw new ApiError(404, 'not_found');
    }
    return message;
  }

  function managed(id: Uint8Array, manageKey: RequestKey): Stored {
    const message = stored.get(idToString(id));
    if (!message || !sameBytes(message.request.manageKey, manageKey.publicKey)) {
      throw new ApiError(404, 'not_found');
    }
    return message;
  }

  function end(message: Stored, state: MessageState): void {
    if (message.state !== 'available' && !(message.state === 'opened' && message.leases.size > 0)) {
      throw new ApiError(410, 'gone', { state: message.state });
    }
    message.state = state;
    message.leases.clear();
    message.endedAt = '2026-10-23T11:00:00Z';
  }

  function envelope(message: Stored) {
    const object = new Uint8Array(message.request.object);
    if (options.tamperObject) {
      object[object.length - 1] = (object.at(-1) ?? 0) ^ 1;
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

  /* eslint-disable @typescript-eslint/require-await -- the fake keeps the async methods of Api, so a thrown ApiError rejects the promise as in the real client */
  const api: Api = {
    async instance(): Promise<Instance> {
      calls.push('instance');
      const id = crypto.getRandomValues(new Uint8Array(16));
      issuedIds.push(id);
      const challenge = encodeBase64url(new Uint8Array(36).fill(challenges.length + 1));
      challenges.push(challenge);
      return {
        ...(options.fileCompression === true && { fileCompression: 'gzip' as const }),
        limits: {
          maxFileBytes: 5_242_880,
          maxTextBytes: 102_400,
          maxObjectBytes: 5_373_952,
          maxTtlSeconds: 604_800,
          defaultTtlSeconds: 86_400,
          maxOpens: 10,
        },
        pow: options.pow?.required ? { ...options.pow, challenge } : { required: false, difficulty: 4 },
        create: { id, idToken: 'token' },
      };
    },

    async transparency() {
      calls.push('transparency');
      throw new Error('the flow tests never load the transparency page');
    },
    async create(request) {
      calls.push('create');
      const failure = failCreate.shift();
      if (failure) {
        throw failure;
      }
      stored.set(idToString(request.id), {
        request,
        state: 'available',
        opensRemaining: request.opensMax,
        failures: 0,
        leases: new Set(),
      });
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
        throw new ApiError(410, 'gone', { state: message.state });
      }
      if (message.request.kdfId !== 0 && !sameBytes(message.request.verifierKey, claimOptions.verifierKey?.publicKey)) {
        message.failures++;
        const locked = message.failures >= (options.lockAfter ?? 100);
        throw new ApiError(403, 'passphrase', {
          retryAfter: locked ? 3600 : 0,
          attemptsLeft: locked ? 0 : Math.max(0, 5 - message.failures),
        });
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
        throw new ApiError(410, 'gone', { state: message.state });
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
    async report(id, readKey, report) {
      calls.push('report');
      const failure = failReport.shift();
      if (failure) {
        throw failure;
      }
      const key = idToString(id);
      const signedByReader = sameBytes(stored.get(key)?.request.readKey, readKey.publicKey);
      const earlier = reports.filter((sent) => sent.id === key && sent.signedByReader);
      if (signedByReader && earlier.length >= REPORTS_PER_MESSAGE) {
        return 'limit';
      }
      reports.push({ id: key, signedByReader, ...report });
      return 'accepted';
    },
    async status(id, manageKey): Promise<Status> {
      calls.push('status');
      const failure = failStatus.shift();
      if (failure) {
        throw failure;
      }
      const message = managed(id, manageKey);
      if (message.endedAt) {
        return {
          state: message.state,
          opensUsed: 0,
          opensMax: 0,
          failedPassphraseAttempts: 0,
          endedAt: message.endedAt,
        };
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
  /* eslint-enable @typescript-eslint/require-await -- only the fake methods above */
  return Object.assign(api, { calls, challenges, stored, issuedIds, reports });
}
