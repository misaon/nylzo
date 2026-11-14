import {
  CHALLENGE_LENGTH,
  encodeBase64url,
  formatShareKey,
  idToString,
  MAX_OBJECT_BYTES,
  newShareKey,
  requestKey,
  requestMessage,
  transferSeconds,
  utf8,
} from '@nylzo/core';
import type { Purpose, RequestComponents, ShareKey } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ResponseError, StoppedError } from './api-errors.ts';
import type { ApiError } from './api-errors.ts';
import { createApi, TRANSFER_TIMEOUT_MS } from './api.ts';
import type { Unlock } from './api.ts';
import { servedPolicy } from './responses.ts';

const randomId = () => crypto.getRandomValues(new Uint8Array(16));

type Handler = (request: Request) => Response | Promise<Response>;

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return Response.json(body, { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

const LEASE = 'A'.repeat(43);
const OBJECT_HEADERS = { 'Content-Type': 'application/octet-stream', 'X-Nylzo-Lease': LEASE };

function fakeServer(handler: Handler) {
  const seen: Request[] = [];
  const seenOptions: (RequestInit & { readonly url: string })[] = [];
  let issued = 0;
  const fetch = async (input: RequestInfo | URL, requestInit?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : input, 'http://nylzo.test');
    const request = new Request(url, requestInit);
    expect(request.headers.get('X-Requested-With')).toBe(request.method === 'POST' ? 'XMLHttpRequest' : null);
    seen.push(request);
    seenOptions.push({ ...requestInit, url: request.url });
    if (new URL(request.url).pathname === '/api/v1/challenge') {
      issued++;
      const challenge = new Uint8Array(CHALLENGE_LENGTH).fill(issued);
      return json(200, { challenge: encodeBase64url(challenge), expires_at: '2026-10-23T10:00:00Z' });
    }
    await expectSignedRequest(request);
    return handler(request);
  };
  const last = () => seen.at(-1) ?? missing('the client sent no request');
  return { api: createApi({ fetch }), seen, seenOptions, last, issued: () => issued };
}

function requestAt(requests: readonly Request[], index: number): Request {
  return requests[index] ?? missing(`the client sent no request ${index}`);
}

function decode(encoded: string): Uint8Array<ArrayBuffer> {
  return new Uint8Array(Buffer.from(encoded, 'base64url'));
}

async function verifies(
  publicKey: string,
  challenge: string,
  signature: string,
  purpose: Purpose,
  method: string,
  path: string,
  body: Uint8Array = new Uint8Array(0),
  components: RequestComponents = {},
): Promise<boolean> {
  const key = await crypto.subtle.importKey('raw', decode(publicKey), 'Ed25519', false, ['verify']);
  const message = await requestMessage(purpose, method, path, decode(challenge), body, components);
  return crypto.subtle.verify('Ed25519', key, decode(signature), message);
}

function authorizationParts(header: string): [string, string, string] {
  const [publicKey = '', challenge = '', signature = ''] = header.replace(/^Nylzo-Sig /u, '').split('.', 3);
  return [publicKey, challenge, signature];
}

async function expectSignedRequest(request: Request): Promise<void> {
  const authorization = request.headers.get('Authorization');
  if (!authorization) {
    return;
  }
  const parts = authorizationParts(authorization);
  const path = new URL(request.url).pathname;
  const purpose = path.endsWith('/status') || path.endsWith('/cancel') ? 'manage' : 'read';
  const components = {
    shareKey: request.headers.get('X-Nylzo-Share-Key') ?? '',
    lease: request.headers.get('X-Nylzo-Lease') ?? '',
    ke1: request.headers.get('X-Nylzo-KE1') ?? '',
    ke3: request.headers.get('X-Nylzo-KE3') ?? '',
  };
  const body = new Uint8Array(await request.clone().arrayBuffer());
  expect(await verifies(...parts, purpose, request.method, path, body, components)).toBe(true);
  expect(request.headers.has('X-Nylzo-Verifier')).toBe(false);
}

const policy = encodeBase64url(utf8(JSON.stringify({ opens_max: 1, ttl_s: 3600, kdf_id: 3, flags: 0 })));
const WRAPPED_SHARE_LENGTH = 1168;
const { publicKey: shareKey } = newShareKey();
const shareKeys = (): ShareKey => ({ seed: new Uint8Array(32).fill(5), publicKey: shareKey });
const wrappedShare = encodeBase64url(new Uint8Array(WRAPPED_SHARE_LENGTH).fill(6));
const HANDSHAKE_CHALLENGE = new Uint8Array(CHALLENGE_LENGTH).fill(42);
const KE3 = new Uint8Array(64).fill(3);
const registrationRequest = new Uint8Array(32).fill(4);

// The last minute before the deadline of instanceBody, as the server issues it (spec 5.6).
const ISSUED_EPOCH = 29_879_219;

// Stands in for the handshake and KE3 of the reader, counting how often a request asked for one.
function unlocker(): Unlock & { readonly count: () => number } {
  let count = 0;
  return Object.assign(
    () => {
      count++;
      return Promise.resolve({ challenge: new Uint8Array(HANDSHAKE_CHALLENGE), ke3: KE3 });
    },
    { count: () => count },
  );
}

const instanceBody = {
  limits: {
    max_file_bytes: 5_242_880,
    max_text_bytes: 102_400,
    max_object_bytes: 5_373_952,
    max_ttl_s: 604_800,
    default_ttl_s: 86_400,
    max_opens: 10,
  },
  pow: { required: true, difficulty: 4, challenge: 'x' },
  create: {
    id: 'AAAAAAAAAAAAAAAAAAAAAA',
    id_token: 'token',
    expires_at: '2026-10-23T10:10:00Z',
    epoch: ISSUED_EPOCH,
    deadline: '2026-10-23T11:00:00Z',
    share: wrappedShare,
    registration: encodeBase64url(new Uint8Array(64).fill(7)),
  },
  versions: { api: 1, envelope: 3, fragment: 3, server: 'dev' },
};

const CHUNKED_OBJECT = Uint8Array.from({ length: 200_000 }, (_, index) => index % 251);

function chunkedStream(object: Uint8Array): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (let offset = 0; offset < object.length; offset += 30_000) {
        controller.enqueue(object.slice(offset, offset + 30_000));
      }
      controller.close();
    },
  });
}

describe('api client', () => {
  it('asks for an id with the TTL, a share key and a registration request, and reads the answers (spec 5.6, 6.6)', async () => {
    const server = fakeServer(() => json(200, instanceBody));
    const instance = await server.api.instance({ ttlSeconds: 3600, shareKey, registrationRequest });
    const request = server.last();
    expect(new URL(request.url).search).toBe('');
    expect(request.headers.get('X-Nylzo-Ttl')).toBe('3600');
    expect(request.headers.get('X-Nylzo-Share-Key')).toBe(formatShareKey(shareKey));
    expect(request.headers.get('X-Nylzo-Registration')).toBe(encodeBase64url(registrationRequest));
    expect(instance.create?.epoch).toBe(ISSUED_EPOCH);
    expect(instance.create?.deadline).toBe(Date.parse('2026-10-23T11:00:00Z') / 1000);
    expect(instance.create?.share).toEqual(new Uint8Array(WRAPPED_SHARE_LENGTH).fill(6));
    expect(instance.create?.registration).toEqual(new Uint8Array(64).fill(7));
    const short = fakeServer(() =>
      json(200, {
        ...instanceBody,
        create: { ...instanceBody.create, registration: encodeBase64url(new Uint8Array(32)) },
      }),
    );
    await expect(short.api.instance({ ttlSeconds: 3600, shareKey, registrationRequest })).rejects.toBeInstanceOf(
      ResponseError,
    );
    await server.api.instance();
    expect(server.last().headers.has('X-Nylzo-Share-Key')).toBe(false);
  });

  it('A-04: refuses a proof-of-work difficulty above the spec maximum before any work', async () => {
    const server = fakeServer(() =>
      json(200, { ...instanceBody, pow: { required: true, difficulty: 9, challenge: 'x' } }),
    );
    await expect(server.api.instance()).rejects.toThrow(ResponseError);
  });

  it('reads the instance and takes no passphrase profile from it', async () => {
    const good = fakeServer(() => json(200, instanceBody));
    const instance = await good.api.instance();
    expect(instance.limits).not.toHaveProperty('defaultKdfId');
    expect(instance.pow).toEqual({ required: true, difficulty: 4, challenge: 'x' });
    expect(instance.create).toBeUndefined();
    const legacy = fakeServer(() =>
      json(200, { ...instanceBody, limits: { ...instanceBody.limits, default_kdf_id: 0 } }),
    );
    const { limits } = await legacy.api.instance();
    expect(limits).not.toHaveProperty('defaultKdfId');
    const malformed = fakeServer(() =>
      json(200, { ...instanceBody, limits: { ...instanceBody.limits, max_opens: 'ten' } }),
    );
    await expect(malformed.api.instance()).rejects.toThrow(ResponseError);
  });

  it('refuses a JSON body larger than 64 KiB, with or without Content-Length (A-02)', async () => {
    const padded = { ...instanceBody, padding: 'x'.repeat(64 * 1024) };
    const declared = fakeServer(() => json(200, padded));
    await expect(declared.api.instance()).rejects.toThrow(ResponseError);
    const streamed = fakeServer(
      () =>
        new Response(new Blob([JSON.stringify(padded)]).stream(), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
    );
    await expect(streamed.api.instance()).rejects.toThrow(ResponseError);
  });

  it('signs metadata with R over a fresh challenge', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(7));
    const server = fakeServer(() =>
      json(200, {
        state: 'available',
        created_at: '2026-10-23T09:00:00Z',
        expires_at: '2026-10-24T09:00:00Z',
        opens_max: 1,
        opens_remaining: 1,
        ttl_s: 86_400,
        kdf_id: 0,
        size: 4127,
      }),
    );
    const metadata = await server.api.metadata(id, readKey);
    expect(metadata).toMatchObject({
      state: 'available',
      opensRemaining: 1,
      kdfId: 0,
      expiresAt: '2026-10-24T09:00:00Z',
    });
    const request = server.last();
    const path = `/api/v1/messages/${idToString(id)}`;
    expect(new URL(request.url).pathname).toBe(path);
    const parts = authorizationParts(request.headers.get('Authorization') ?? '');
    expect(await verifies(...parts, 'read', 'GET', path)).toBe(true);
    expect(request.headers.get('X-Nylzo-Verifier')).toBeNull();
  });

  it('burns with R over the challenge of its handshake and cancels and reads the status with M, each without a body', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(7));
    const unlock = unlocker();
    const manageKey = await requestKey(new Uint8Array(32).fill(9));
    const server = fakeServer((request) =>
      new URL(request.url).pathname.endsWith('/status')
        ? json(200, {
            state: 'cancelled',
            opens_used: 0,
            opens_max: 1,
            failed_passphrase_attempts: 2,
            ended_at: '2026-10-23T10:00:00Z',
          })
        : new Response(null, { status: 204 }),
    );
    await server.api.burn(id, readKey, unlock);
    await server.api.cancel(id, manageKey);
    expect(await server.api.status(id, manageKey)).toEqual({
      state: 'cancelled',
      opensUsed: 0,
      opensMax: 1,
      failedPassphraseAttempts: 2,
      endedAt: '2026-10-23T10:00:00Z',
    });
    const signed = server.seen.filter((request) => !request.url.endsWith('/challenge'));
    const base = `/api/v1/messages/${idToString(id)}`;
    const expected = [
      ['POST', `${base}/burn`, 'read', readKey, encodeBase64url(KE3)],
      ['POST', `${base}/cancel`, 'manage', manageKey, null],
      ['GET', `${base}/status`, 'manage', manageKey, null],
    ] as const;
    expect(signed).toHaveLength(expected.length);
    for (const [index, [method, path, purpose, key, ke3]] of expected.entries()) {
      const request = requestAt(signed, index);
      expect([request.method, new URL(request.url).pathname]).toEqual([method, path]);
      const parts = authorizationParts(request.headers.get('Authorization') ?? '');
      expect(parts[0]).toBe(encodeBase64url(key.publicKey));
      expect(await verifies(...parts, purpose, method, path, undefined, { ke3: ke3 ?? '' })).toBe(true);
      expect(request.headers.get('X-Nylzo-KE3')).toBe(ke3);
      expect(await request.arrayBuffer()).toHaveProperty('byteLength', 0);
    }
    expect(authorizationParts(requestAt(signed, 0).headers.get('Authorization') ?? '')[1]).toBe(
      encodeBase64url(HANDSHAKE_CHALLENGE),
    );
    expect(unlock.count()).toBe(1);
  });

  it.each([
    ['an object of another media type', { 'Content-Type': 'text/html', 'X-Nylzo-Lease': LEASE }],
    ['a lease that is not 32 canonical bytes', { 'Content-Type': 'application/octet-stream', 'X-Nylzo-Lease': 'l' }],
    ['no lease', { 'Content-Type': 'application/octet-stream' }],
  ])('refuses a claim answer with %s', async (_case, headers) => {
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const server = fakeServer(
      () =>
        new Response(new Uint8Array(16), {
          status: 200,
          headers: { ...headers, 'X-Nylzo-Policy': policy, 'X-Nylzo-Share': wrappedShare },
        }),
    );
    const sent = shareKeys();
    await expect(
      server.api.claim(randomId(), readKey, { unlock: unlocker(), shareKeys: () => sent }),
    ).rejects.toBeInstanceOf(ResponseError);
    expect(sent.seed.every((byte) => byte === 0)).toBe(true);
  });

  it('reads JSON only as application/json and Retry-After only as plain seconds', async () => {
    const server = fakeServer(() =>
      Response.json({ limits: {} }, { status: 200, headers: { 'Content-Type': 'text/plain' } }),
    );
    await expect(server.api.instance()).rejects.toBeInstanceOf(ResponseError);
    for (const [header, expected] of [
      ['30', 30],
      ['0x10', undefined],
      ['1e3', undefined],
      [' 7 ', 7],
    ] as const) {
      const limited = fakeServer(() => new Response('', { status: 429, headers: { 'Retry-After': header } }));
      await expect(limited.api.instance()).rejects.toMatchObject({ kind: 'unexpected', retryAfter: expected });
    }
  });

  it('claims with R and KE3 over the challenge of its handshake and streams the object', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const unlock = unlocker();
    const object = new Uint8Array(70_000).fill(9);
    const server = fakeServer(
      () =>
        new Response(object, {
          status: 200,
          headers: {
            'Content-Type': 'application/octet-stream',
            'X-Nylzo-Lease': LEASE,
            'X-Nylzo-Policy': policy,
            'X-Nylzo-Share': wrappedShare,
            'Content-Length': String(object.length),
          },
        }),
    );
    const progress: [number, number | undefined][] = [];
    const leases: string[] = [];
    const claimed = await server.api.claim(id, readKey, {
      unlock,
      shareKeys,
      onLease: (lease) => {
        leases.push(lease);
      },
      onProgress: (received, total) => {
        progress.push([received, total]);
      },
    });
    expect(leases).toEqual([LEASE]);
    expect(claimed.lease).toBe(LEASE);
    expect(claimed.policy).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 3, flags: 0 });
    expect(claimed.share).toEqual(new Uint8Array(WRAPPED_SHARE_LENGTH).fill(6));
    expect(claimed.object).toEqual(object);
    expect(progress.at(-1)).toEqual([object.length, object.length]);
    const request = server.last();
    const authorization = request.headers.get('Authorization') ?? '';
    expect(request.method).toBe('POST');
    const [, challenge] = authorizationParts(authorization);
    expect(challenge).toBe(encodeBase64url(HANDSHAKE_CHALLENGE));
    expect(request.headers.get('X-Nylzo-KE3')).toBe(encodeBase64url(KE3));
    expect(request.headers.get('X-Nylzo-Share-Key')).toBe(formatShareKey(shareKey));
    expect(request.headers.get('X-Nylzo-Flags')).toBe('2');
    expect(server.issued()).toBe(0);
  });

  it('sends the ack as one keepalive request when its challenge was fetched before the message showed', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(3));
    const server = fakeServer(() => new Response(null, { status: 204 }));
    const challenge = await server.api.challenge();
    server.seen.length = 0;
    await server.api.ack(randomId(), readKey, LEASE, challenge);
    expect(server.seen.map((request) => new URL(request.url).pathname)).toEqual([expect.stringMatching(/\/ack$/u)]);
    expect(server.seenOptions.at(-1)?.keepalive).toBe(true);
  });

  it('signs the retry of an ack with a new challenge when the server refused the prepared one', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(3));
    const used: string[] = [];
    const server = fakeServer((request) => {
      used.push(authorizationParts(request.headers.get('Authorization') ?? '')[1]);
      return used.length === 1 ? json(404, { error: 'not_found' }) : new Response(null, { status: 204 });
    });
    const prepared = await server.api.challenge();
    await server.api.ack(randomId(), readKey, LEASE, prepared);
    expect(server.issued()).toBe(2);
    expect(used).toEqual([
      encodeBase64url(new Uint8Array(CHALLENGE_LENGTH).fill(1)),
      encodeBase64url(new Uint8Array(CHALLENGE_LENGTH).fill(2)),
    ]);
  });

  it('maps errors from the server', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(4));
    const cases: [Response, Partial<ApiError>][] = [
      [
        json(403, { error: 'passphrase', retry_after: 60, attempts_left: 0 }),
        { kind: 'passphrase', retryAfter: 60, attemptsLeft: 0 },
      ],
      [json(410, { error: 'gone', state: 'opened' }), { kind: 'gone', state: 'opened' }],
      [json(400, { error: 'client_outdated' }), { kind: 'client_outdated', status: 400 }],
      [
        json(429, { error: 'rate_limited', retry_after: 5 }, { 'Retry-After': '5' }),
        { kind: 'rate_limited', retryAfter: 5 },
      ],
      [json(503, { error: 'unavailable' }, { 'Retry-After': '30' }), { kind: 'unavailable', retryAfter: 30 }],
      [new Response('<html>', { status: 502 }), { kind: 'unexpected', status: 502 }],
    ];
    for (const [response, expected] of cases) {
      const server = fakeServer(() => response.clone());
      await expect(server.api.claim(randomId(), readKey, { unlock: unlocker(), shareKeys })).rejects.toMatchObject(
        expected,
      );
    }
  });

  it('sends the create header and the proof of work', async () => {
    const server = fakeServer(() => json(201, { id: 'AAAAAAAAAAAAAAAAAAAAAA', expires_at: '2026-10-24T09:00:00Z' }));
    const object = new Uint8Array(4127);
    const keys = {
      id: randomId(),
      idToken: 'token',
      epoch: 493_000,
      deadline: Date.parse('2026-10-24T09:00:00Z') / 1000,
      readKey: new Uint8Array(32).fill(1),
      registration: new Uint8Array(192).fill(2),
      manageKey: new Uint8Array(32).fill(3),
    };
    await server.api.create({
      ...keys,
      kdfId: 0,
      opensMax: 1,
      ttlSeconds: 3600,
      object,
      powToken: 'chal.0000000000000003',
    });
    const request = server.last();
    const header: unknown = JSON.parse(
      Buffer.from(request.headers.get('X-Nylzo-Create') ?? '', 'base64url').toString(),
    );
    expect(header).toEqual({
      id: idToString(keys.id),
      id_token: 'token',
      epoch: 493_000,
      read_key: encodeBase64url(keys.readKey),
      registration: encodeBase64url(keys.registration),
      manage_key: encodeBase64url(keys.manageKey),
      kdf_id: 0,
      opens_max: 1,
      ttl_s: 3600,
      size: object.length,
    });
    expect(request.headers.get('X-Nylzo-Pow')).toBe('chal.0000000000000003');
    expect(request.headers.get('Content-Type')).toBe('application/octet-stream');
    expect(new Uint8Array(await request.arrayBuffer())).toEqual(object);
  });

  it('accepts only a canonical policy header with integer fields', () => {
    expect(servedPolicy(policy)).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 3, flags: 0 });
    for (const malformed of [
      null,
      '',
      `${policy}A`,
      `${policy}=`,
      encodeBase64url(utf8('[]')),
      encodeBase64url(utf8('{"opens_max":1}')),
    ]) {
      expect(() => servedPolicy(malformed)).toThrow(ResponseError);
    }
  });

  it('refuses a timestamp that is not RFC 3339', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(5));
    const server = fakeServer(() =>
      json(200, {
        state: 'available',
        created_at: 'yesterday',
        expires_at: '2026-10-24T09:00:00Z',
        opens_max: 1,
        opens_remaining: 1,
        ttl_s: 86_400,
        kdf_id: 0,
        size: 1,
      }),
    );
    await expect(server.api.metadata(randomId(), readKey)).rejects.toThrow(ResponseError);
  });

  it('leaves retryAfter empty when the server sends no Retry-After', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(6));
    const server = fakeServer(() => json(503, { error: 'unavailable' }));
    await expect(server.api.claim(randomId(), readKey, { unlock: unlocker(), shareKeys })).rejects.toMatchObject({
      kind: 'unavailable',
      retryAfter: undefined,
    });
  });

  it('refuses an object larger than the protocol allows', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(8));
    const server = fakeServer(
      () =>
        new Response(new Uint8Array(16), {
          status: 200,
          headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy, 'Content-Length': String(MAX_OBJECT_BYTES + 1) },
        }),
    );
    await expect(server.api.claim(randomId(), readKey, { unlock: unlocker(), shareKeys })).rejects.toThrow(
      ResponseError,
    );
  });

  it('sends no claim once the caller stops it after the challenge', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const server = fakeServer(() => json(500, { error: 'internal' }));
    const unlock = unlocker();
    let unlocked = false;
    const proceed = () => {
      unlocked = unlock.count() > 0;
      return false;
    };
    await expect(server.api.claim(randomId(), readKey, { unlock, shareKeys, proceed })).rejects.toThrow(StoppedError);
    expect(unlocked).toBe(true);
    expect(server.seen).toEqual([]);
  });

  it('sends every request without credentials, cache, referrer or redirects', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const object = new Uint8Array(100).fill(1);
    const server = fakeServer((request) => {
      const path = new URL(request.url).pathname;
      if (path === '/api/v1/instance') {
        return json(200, instanceBody);
      }
      if (path === '/api/v1/messages') {
        return json(201, { id: 'AAAAAAAAAAAAAAAAAAAAAA', expires_at: '2026-10-24T09:00:00Z' });
      }
      if (path.endsWith('/handshake')) {
        return json(200, {
          ke2: encodeBase64url(new Uint8Array(320)),
          challenge: encodeBase64url(HANDSHAKE_CHALLENGE),
          expires_at: '2026-10-23T10:01:00Z',
          attempts_left: 5,
          retry_after: 0,
        });
      }
      if (path.endsWith('/claim') || path.endsWith('/blob')) {
        return new Response(object, { status: 200, headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy } });
      }
      return new Response(null, { status: 204 });
    });
    const id = randomId();
    await server.api.instance();
    await server.api.create({
      id,
      idToken: 't',
      epoch: 1,
      deadline: Date.parse('2026-10-24T09:00:00Z') / 1000,
      readKey: readKey.publicKey,
      registration: new Uint8Array(192),
      manageKey: readKey.publicKey,
      kdfId: 0,
      opensMax: 1,
      ttlSeconds: 3600,
      object,
    });
    await server.api.handshake(id, readKey, new Uint8Array(96));
    await server.api.claim(id, readKey, { unlock: unlocker(), shareKeys });
    await server.api.blob(id, readKey, LEASE, shareKeys);
    await server.api.ack(id, readKey, LEASE);
    await server.api.burn(id, readKey, unlocker());
    expect(new Set(server.seen.map((request) => new URL(request.url).pathname)).size).toBe(8);
    expect(
      server.seenOptions.map((request) => ({
        url: request.url,
        redirect: request.redirect,
        referrerPolicy: request.referrerPolicy,
        credentials: request.credentials,
        cache: request.cache,
      })),
    ).toEqual(
      server.seen.map((request) => ({
        url: request.url,
        redirect: 'error',
        referrerPolicy: 'no-referrer',
        credentials: 'omit',
        cache: 'no-store',
      })),
    );
    expect(
      server.seenOptions
        .filter((request) => request.keepalive === true)
        .map((request) => new URL(request.url).pathname),
      'only the ack outlives a page that is left',
    ).toEqual([expect.stringMatching(/^\/api\/v1\/messages\/[\w-]{22}\/ack$/u)]);
  });

  it('stops reading an object without Content-Length once it passes the protocol maximum', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const chunk = new Uint8Array(1 << 20);
    let sent = 0;
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (sent > 2 * MAX_OBJECT_BYTES) {
          controller.close();
          return;
        }
        sent += chunk.length;
        controller.enqueue(chunk);
      },
    });
    const server = fakeServer(
      () => new Response(body, { status: 200, headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy } }),
    );
    await expect(server.api.claim(randomId(), readKey, { unlock: unlocker(), shareKeys })).rejects.toThrow(
      ResponseError,
    );
    expect(sent).toBeLessThanOrEqual(MAX_OBJECT_BYTES + 2 * chunk.length);
  });

  it.each([
    ['no', undefined],
    ['an exact', CHUNKED_OBJECT.length],
    ['a wrong', 1000],
  ])('reads an object in chunks with %s Content-Length', async (_label, length) => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const headers: Record<string, string> = {
      'Content-Type': 'application/octet-stream',
      'X-Nylzo-Policy': policy,
      ...(length !== undefined && { 'Content-Length': String(length) }),
    };
    const progress: [number, number | undefined][] = [];
    const server = fakeServer(() => new Response(chunkedStream(CHUNKED_OBJECT), { status: 200, headers }));
    const envelope = await server.api.blob(randomId(), readKey, LEASE, shareKeys, {
      onProgress: (received, total) => {
        progress.push([received, total]);
      },
    });
    expect(envelope.object).toEqual(CHUNKED_OBJECT);
    expect(envelope.object.buffer.byteLength).toBe(CHUNKED_OBJECT.length);
    expect(progress.at(-1)).toEqual([CHUNKED_OBJECT.length, length]);
  });

  it('gives a transfer the time the protocol allows the largest object, plus a margin', () => {
    expect(TRANSFER_TIMEOUT_MS).toBeGreaterThan(transferSeconds(MAX_OBJECT_BYTES) * 1000);
    expect(TRANSFER_TIMEOUT_MS).toBe(360_000);
  });
});

const DIGEST = 'a'.repeat(64);

function transparencyReport(change: Record<string, unknown> = {}) {
  return {
    server: 'v1.2.3',
    operator: { name: 'Example s.r.o.', contact: 'abuse@example.org' },
    limits: {
      max_file_bytes: 5_242_880,
      max_text_bytes: 102_400,
      max_object_bytes: 5_373_952,
      max_ttl_s: 604_800,
      default_ttl_s: 86_400,
      max_opens: 10,
    },
    rates: {
      create_per_hour: 20,
      create_per_day: 100,
      create_bytes_per_day: 50_000_000,
      read_per_hour: 300,
      abuse_per_hour: 5,
      coarse_multiplier: 5,
      global_daily_creates: 0,
      pow_difficulty: 4,
    },
    retention: { tombstone_s: 604_800, abuse_report_s: 604_800, daily_counter_s: 86_400 },
    ...change,
  };
}

function clientManifest(change: Record<string, unknown> = {}) {
  return {
    server: 'v1.2.3',
    built: true,
    nonce_placeholder: '__NYLZO_CSP_NONCE__',
    pages: ['/'],
    files: [{ path: '/', sha256: DIGEST }],
    ...change,
  };
}

function serveTransparency(report: unknown, manifest: unknown) {
  return fakeServer((request) =>
    json(200, new URL(request.url).pathname === '/.well-known/nylzo/client.json' ? manifest : report),
  ).api.transparency();
}

describe('share keys of refused downloads', () => {
  it('erases the share key seed when a download answer is refused', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const server = fakeServer(
      () =>
        new Response(new Uint8Array(16), {
          status: 200,
          headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy, 'X-Nylzo-Share': 'malformed' },
        }),
    );
    const sent = shareKeys();
    await expect(server.api.blob(randomId(), readKey, LEASE, () => sent)).rejects.toBeInstanceOf(ResponseError);
    expect(sent.seed.every((byte) => byte === 0)).toBe(true);
  });
});

describe('transparency', () => {
  it('reads the report and the client manifest', async () => {
    const transparency = await serveTransparency(transparencyReport(), clientManifest());
    expect(transparency.server).toBe('v1.2.3');
    expect(transparency.operator).toEqual({ name: 'Example s.r.o.', contact: 'abuse@example.org' });
    expect(transparency.limits.maxOpens).toBe(10);
    expect(transparency.rates.globalDailyCreates).toBe(0);
    expect(transparency.retention.tombstoneSeconds).toBe(604_800);
    expect(transparency.files).toEqual([{ path: '/', sha256: DIGEST }]);
  });

  it('accepts an instance without an operator', async () => {
    const { operator } = await serveTransparency(transparencyReport({ operator: undefined }), clientManifest());
    expect(operator).toBeUndefined();
  });

  it('refuses malformed details', async () => {
    for (const [report, manifest] of [
      [transparencyReport({ server: 'v9' }), clientManifest()],
      [transparencyReport(), clientManifest({ files: [{ path: '/', sha256: 'A'.repeat(64) }] })],
      [transparencyReport(), clientManifest({ files: [{ path: 'assets/x.js', sha256: DIGEST }] })],
      [transparencyReport(), clientManifest({ built: 'yes' })],
      [
        transparencyReport(),
        clientManifest({
          files: [
            { path: '/', sha256: DIGEST },
            { path: '/', sha256: DIGEST },
          ],
        }),
      ],
      [transparencyReport({ operator: { name: 'x'.repeat(201) } }), clientManifest()],
      [transparencyReport({ operator: { contact: 7 } }), clientManifest()],
      [transparencyReport({ operator: 'Example' }), clientManifest()],
      [transparencyReport({ rates: {} }), clientManifest()],
    ]) {
      await expect(serveTransparency(report, manifest)).rejects.toBeInstanceOf(ResponseError);
    }
  });
});

function policyWith(changed: object): string {
  return encodeBase64url(utf8(JSON.stringify({ opens_max: 1, ttl_s: 3600, kdf_id: 3, ...changed })));
}

describe('F13: deadlines', () => {
  it('reads the deadline of X-Nylzo-Policy exactly when its flags seal one', () => {
    for (const changed of [
      { flags: 2 },
      { flags: 0, deadline: 1_797_000_000 },
      { flags: 2, deadline: 0 },
      { flags: 2, deadline: 2 ** 32 },
    ]) {
      expect(() => servedPolicy(policyWith(changed))).toThrow(ResponseError);
    }
    expect(servedPolicy(policyWith({ flags: 2, deadline: 1_797_000_000 }))).toEqual({
      opensMax: 1,
      ttlSeconds: 3600,
      kdfId: 3,
      flags: 2,
      deadline: 1_797_000_000,
    });
  });

  it.each([
    ['no deadline', { deadline: undefined }],
    ['a deadline before the TTL', { deadline: '2026-10-23T10:59:00Z', epoch: ISSUED_EPOCH - 1 }],
    ['a deadline more than a minute after the TTL', { deadline: '2026-10-23T11:02:00Z', epoch: ISSUED_EPOCH + 2 }],
    ['a deadline inside a minute', { deadline: '2026-10-23T11:00:30Z' }],
    ['an epoch of another minute', { epoch: ISSUED_EPOCH + 1 }],
  ])('F13: refuses an id with %s', async (_, changed) => {
    const server = fakeServer(() => json(200, { ...instanceBody, create: { ...instanceBody.create, ...changed } }));
    await expect(server.api.instance({ ttlSeconds: 3600, shareKey, registrationRequest })).rejects.toThrow(
      ResponseError,
    );
  });

  it('F13: accepts a deadline rounded up to the next minute', async () => {
    const changed = { deadline: '2026-10-23T11:01:00Z', epoch: ISSUED_EPOCH + 1 };
    const server = fakeServer(() => json(200, { ...instanceBody, create: { ...instanceBody.create, ...changed } }));
    const instance = await server.api.instance({ ttlSeconds: 3600, shareKey, registrationRequest });
    expect(instance.create?.epoch).toBe(ISSUED_EPOCH + 1);
  });

  it('F13: refuses a created message that expires at another time than its sealed deadline', async () => {
    const server = fakeServer(() => json(201, { id: 'AAAAAAAAAAAAAAAAAAAAAA', expires_at: '2026-10-24T09:01:00Z' }));
    const request = {
      id: randomId(),
      idToken: 'token',
      epoch: 1,
      deadline: Date.parse('2026-10-24T09:00:00Z') / 1000,
      readKey: new Uint8Array(32).fill(1),
      registration: new Uint8Array(192).fill(2),
      manageKey: new Uint8Array(32).fill(3),
      kdfId: 0 as const,
      opensMax: 1,
      ttlSeconds: 3600,
      object: new Uint8Array(4127),
    };
    await expect(server.api.create(request)).rejects.toThrow(ResponseError);
  });
});

function missing(message: string): never {
  throw new Error(message);
}

describe('api client handshake and retries (spec 5.5, 6.4)', () => {
  it('retries a 404 once with a new challenge and then reports it', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(3));
    let calls = 0;
    const flaky = fakeServer(() =>
      ++calls === 1 ? json(404, { error: 'not_found' }) : new Response(null, { status: 204 }),
    );
    await flaky.api.ack(randomId(), readKey, 'lease');
    expect(flaky.issued()).toBe(2);
    const gone = fakeServer(() => json(404, { error: 'not_found' }));
    await expect(gone.api.ack(randomId(), readKey, 'lease')).rejects.toMatchObject({ kind: 'not_found', status: 404 });
    expect(gone.issued()).toBe(2);
  });

  it('draws a new share key for the retry after a 404 and reports the one the answer is wrapped to', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(3));
    let attempt = 0;
    const drawn: ShareKey[] = [];
    const sent: (string | null)[] = [];
    const flaky = fakeServer((request) => {
      sent.push(request.headers.get('X-Nylzo-Share-Key'));
      return ++attempt === 1
        ? json(404, { error: 'not_found' })
        : new Response(new Uint8Array(16), {
            status: 200,
            headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy, 'X-Nylzo-Share': wrappedShare },
          });
    });
    const unlock = unlocker();
    const claimed = await flaky.api.claim(randomId(), readKey, {
      unlock,
      shareKeys: () => {
        const key = newShareKey();
        drawn.push(key);
        return key;
      },
    });
    expect(sent).toEqual(drawn.map((key) => formatShareKey(key.publicKey)));
    expect(new Set(sent).size).toBe(2);
    expect(claimed.shareKey).toBe(drawn[1]);
    expect(drawn.map((key) => key.seed.every((byte) => byte === 0))).toEqual([true, false]);
    expect(unlock.count()).toBe(2);
  });

  it('sends KE1 with a signed handshake and reads KE2, its challenge and the attempts', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const ke2 = new Uint8Array(320).fill(5);
    const answer = { ke2: encodeBase64url(ke2), challenge: encodeBase64url(HANDSHAKE_CHALLENGE) };
    const server = fakeServer(() =>
      json(200, { ...answer, expires_at: '2026-10-23T10:01:00Z', attempts_left: 4, retry_after: 0 }),
    );
    const ke1 = new Uint8Array(96).fill(2);
    expect(await server.api.handshake(id, readKey, ke1)).toEqual({
      ke2,
      challenge: HANDSHAKE_CHALLENGE,
      attemptsLeft: 4,
      retryAfter: 0,
    });
    const request = server.last();
    expect(new URL(request.url).pathname).toBe(`/api/v1/messages/${idToString(id)}/handshake`);
    expect(request.headers.get('X-Nylzo-KE1')).toBe(encodeBase64url(ke1));
    const short = fakeServer(() =>
      json(200, { ...answer, ke2: encodeBase64url(ke2.subarray(1)), attempts_left: 4, retry_after: 0 }),
    );
    await expect(short.api.handshake(id, readKey, ke1)).rejects.toBeInstanceOf(ResponseError);
  });

  it('names the lease of a resumed open in the signed handshake and resumes the download with KE3 (spec 6.4, 6.5)', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const server = fakeServer((request) =>
      new URL(request.url).pathname.endsWith('/handshake')
        ? json(200, {
            ke2: encodeBase64url(new Uint8Array(320)),
            challenge: encodeBase64url(HANDSHAKE_CHALLENGE),
            expires_at: '2026-10-23T10:01:00Z',
            attempts_left: 4,
            retry_after: 0,
          })
        : new Response(new Uint8Array(16), {
            status: 200,
            headers: { ...OBJECT_HEADERS, 'X-Nylzo-Policy': policy, 'X-Nylzo-Share': wrappedShare },
          }),
    );
    await server.api.handshake(id, readKey, new Uint8Array(96), LEASE);
    expect(server.last().headers.get('X-Nylzo-Lease')).toBe(LEASE);
    const unlock = unlocker();
    await server.api.blob(id, readKey, LEASE, shareKeys, { unlock });
    const download = server.last();
    expect(authorizationParts(download.headers.get('Authorization') ?? '')[1]).toBe(
      encodeBase64url(HANDSHAKE_CHALLENGE),
    );
    expect(download.headers.get('X-Nylzo-KE3')).toBe(encodeBase64url(KE3));
    expect(download.headers.get('X-Nylzo-Lease')).toBe(LEASE);
    expect(unlock.count()).toBe(1);
    expect(server.issued()).toBe(1);
    await server.api.blob(id, readKey, LEASE, shareKeys);
    expect(server.last().headers.has('X-Nylzo-KE3')).toBe(false);
    expect(server.issued()).toBe(2);
  });
});
