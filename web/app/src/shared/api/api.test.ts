import {
  CHALLENGE_LENGTH,
  encodeBase64url,
  idToString,
  MAX_OBJECT_BYTES,
  type Purpose,
  requestKey,
  requestMessage,
  transferSeconds,
  utf8,
} from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { type ApiError, ResponseError, StoppedError } from './api-errors.ts';
import { createApi, TRANSFER_TIMEOUT_MS } from './api.ts';
import { abuseReport } from './fake-api.ts';
import { servedPolicy } from './responses.ts';

const randomId = () => crypto.getRandomValues(new Uint8Array(16));

type Handler = (request: Request) => Response | Promise<Response>;

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return Response.json(body, { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

function fakeServer(handler: Handler) {
  const seen: Request[] = [];
  let issued = 0;
  const fetch = async (input: RequestInfo | URL, requestInit?: RequestInit) => {
    const url = new URL(input instanceof Request ? input.url : input, 'http://nylzo.test');
    const request = new Request(url, requestInit);
    seen.push(request);
    if (new URL(request.url).pathname === '/api/v1/challenge') {
      issued++;
      const challenge = new Uint8Array(CHALLENGE_LENGTH).fill(issued);
      return json(200, { challenge: encodeBase64url(challenge), expires_at: '2026-10-23T10:00:00Z' });
    }
    return handler(request);
  };
  const last = () => seen.at(-1) ?? expect.unreachable('the client sent no request');
  return { api: createApi({ fetch }), seen, last, issued: () => issued };
}

function requestAt(requests: readonly Request[], index: number): Request {
  return requests[index] ?? expect.unreachable(`the client sent no request ${index}`);
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
): Promise<boolean> {
  const key = await crypto.subtle.importKey('raw', decode(publicKey), 'Ed25519', false, ['verify']);
  const message = await requestMessage(purpose, method, path, decode(challenge), body);
  return crypto.subtle.verify('Ed25519', key, decode(signature), message);
}

function authorizationParts(header: string): [string, string, string] {
  const [publicKey = '', challenge = '', signature = ''] = header.replace(/^Nylzo-Sig /, '').split('.', 3);
  return [publicKey, challenge, signature];
}

const policy = encodeBase64url(utf8(JSON.stringify({ opens_max: 1, ttl_s: 3600, kdf_id: 1, flags: 0 })));

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
  create: { id: 'AAAAAAAAAAAAAAAAAAAAAA', id_token: 'token', expires_at: '2026-10-23T10:10:00Z' },
  versions: { api: 1, envelope: 1, fragment: 1, server: 'dev' },
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
  it.each([undefined, 'gzip', 'future'])(
    'negotiates the optional file compression capability: %s',
    async (encoding) => {
      const server = fakeServer(() => json(200, { ...instanceBody, file_compression: encoding }));
      const instance = await server.api.instance();
      expect(instance.fileCompression).toBe(encoding === 'gzip' ? 'gzip' : undefined);
    },
  );

  it('reads the instance and takes no passphrase profile from it', async () => {
    const good = fakeServer(() => json(200, instanceBody));
    const instance = await good.api.instance();
    expect(instance.limits).not.toHaveProperty('defaultKdfId');
    expect(instance.pow).toEqual({ required: true, difficulty: 4, challenge: 'x' });
    expect(idToString(instance.create.id)).toBe('AAAAAAAAAAAAAAAAAAAAAA');
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
        passphrase_required: false,
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

  it('burns with R and cancels and reads the status with M, each without a body', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(7));
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
    await server.api.burn(id, readKey);
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
      ['POST', `${base}/burn`, 'read', readKey],
      ['POST', `${base}/cancel`, 'manage', manageKey],
      ['GET', `${base}/status`, 'manage', manageKey],
    ] as const;
    expect(signed).toHaveLength(expected.length);
    for (const [index, [method, path, purpose, key]] of expected.entries()) {
      const request = requestAt(signed, index);
      expect([request.method, new URL(request.url).pathname]).toEqual([method, path]);
      const parts = authorizationParts(request.headers.get('Authorization') ?? '');
      expect(parts[0]).toBe(encodeBase64url(key.publicKey));
      expect(await verifies(...parts, purpose, method, path)).toBe(true);
      expect(await request.arrayBuffer()).toHaveProperty('byteLength', 0);
    }
  });

  it('signs an abuse report with R over its JSON body', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(7));
    const server = fakeServer(() => json(202, { status: 'accepted' }));
    expect(await server.api.report(id, readKey, abuseReport('phishing "quoted" ✓', 'jana@example.cz'))).toBe(
      'accepted',
    );
    await server.api.report(id, readKey, { reason: '', contact: '', goodFaith: false });
    const reports = server.seen.filter((request) => request.url.endsWith('/api/v1/abuse'));
    const bodies = [];
    for (const request of reports) {
      expect(request.method).toBe('POST');
      expect(request.headers.get('Content-Type')).toBe('application/json');
      const body = new Uint8Array(await request.arrayBuffer());
      bodies.push(JSON.parse(new TextDecoder().decode(body)));
      const parts = authorizationParts(request.headers.get('Authorization') ?? '');
      expect(await verifies(...parts, 'read', 'POST', '/api/v1/abuse', body)).toBe(true);
      expect(await verifies(...parts, 'read', 'POST', '/api/v1/abuse')).toBe(false);
    }
    expect(bodies).toEqual([
      { id: idToString(id), reason: 'phishing "quoted" ✓', contact: 'jana@example.cz', good_faith: true },
      { id: idToString(id) },
    ]);
  });

  it('tells a report the daily cap dropped from one the server took (spec §6.6)', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(7));
    const answers = [{ status: 'limit' }, { status: 'stored' }, {}];
    const server = fakeServer(() => json(202, answers.shift()));
    expect(await server.api.report(randomId(), readKey, abuseReport(''))).toBe('limit');
    await expect(server.api.report(randomId(), readKey, abuseReport(''))).rejects.toThrow(ResponseError);
    await expect(server.api.report(randomId(), readKey, abuseReport(''))).rejects.toThrow(ResponseError);
  });

  it('claims with R and V over the same challenge and streams the object', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const verifierKey = await requestKey(new Uint8Array(32).fill(2));
    const object = new Uint8Array(70_000).fill(9);
    const server = fakeServer(
      () =>
        new Response(object, {
          status: 200,
          headers: {
            'X-Nylzo-Lease': 'lease-token',
            'X-Nylzo-Policy': policy,
            'Content-Length': String(object.length),
          },
        }),
    );
    const progress: [number, number | undefined][] = [];
    const leases: string[] = [];
    const claimed = await server.api.claim(id, readKey, {
      verifierKey,
      onLease: (lease) => {
        leases.push(lease);
      },
      onProgress: (received, total) => {
        progress.push([received, total]);
      },
    });
    expect(leases).toEqual(['lease-token']);
    expect(claimed.lease).toBe('lease-token');
    expect(claimed.policy).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 1, flags: 0 });
    expect(claimed.object).toEqual(object);
    expect(progress.at(-1)).toEqual([object.length, object.length]);
    const request = server.last();
    const path = `/api/v1/messages/${idToString(id)}/claim`;
    const authorization = request.headers.get('Authorization') ?? '';
    const verifier = request.headers.get('X-Nylzo-Verifier') ?? '';
    expect(request.method).toBe('POST');
    const [publicKey, challenge, signature] = authorizationParts(authorization);
    const [verifierPublicKey = '', verifierSignature = '', ...rest] = verifier.split('.');
    expect(rest).toEqual([]);
    expect(challenge).toBe(encodeBase64url(new Uint8Array(CHALLENGE_LENGTH).fill(1)));
    expect(await verifies(publicKey, challenge, signature, 'read', 'POST', path)).toBe(true);
    expect(await verifies(verifierPublicKey, challenge, verifierSignature, 'verifier', 'POST', path)).toBe(true);
    expect(verifierPublicKey).toBe(encodeBase64url(verifierKey.publicKey));
  });

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

  it('maps errors from the server', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(4));
    const cases: [Response, Partial<ApiError>][] = [
      [
        json(403, { error: 'passphrase', retry_after: 60, attempts_left: 0 }),
        { kind: 'passphrase', retryAfter: 60, attemptsLeft: 0 },
      ],
      [json(410, { error: 'gone', state: 'opened' }), { kind: 'gone', state: 'opened' }],
      [
        json(429, { error: 'rate_limited', retry_after: 5 }, { 'Retry-After': '5' }),
        { kind: 'rate_limited', retryAfter: 5 },
      ],
      [json(503, { error: 'unavailable' }, { 'Retry-After': '30' }), { kind: 'unavailable', retryAfter: 30 }],
      [new Response('<html>', { status: 502 }), { kind: 'unexpected', status: 502 }],
    ];
    for (const [response, expected] of cases) {
      const server = fakeServer(() => response.clone());
      await expect(server.api.claim(randomId(), readKey)).rejects.toMatchObject(expected);
    }
  });

  it('sends the create header and the proof of work', async () => {
    const server = fakeServer(() => json(201, { id: 'AAAAAAAAAAAAAAAAAAAAAA', expires_at: '2026-10-24T09:00:00Z' }));
    const object = new Uint8Array(4127);
    const keys = {
      id: randomId(),
      idToken: 'token',
      readKey: new Uint8Array(32).fill(1),
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
      read_key: encodeBase64url(keys.readKey),
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
    expect(servedPolicy(policy)).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 1, flags: 0 });
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
        passphrase_required: false,
        kdf_id: 0,
        size: 1,
      }),
    );
    await expect(server.api.metadata(randomId(), readKey)).rejects.toThrow(ResponseError);
  });

  it('leaves retryAfter empty when the server sends no Retry-After', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(6));
    const server = fakeServer(() => json(503, { error: 'unavailable' }));
    await expect(server.api.claim(randomId(), readKey)).rejects.toMatchObject({
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
          headers: { 'X-Nylzo-Lease': 'l', 'X-Nylzo-Policy': policy, 'Content-Length': String(MAX_OBJECT_BYTES + 1) },
        }),
    );
    await expect(server.api.claim(randomId(), readKey)).rejects.toThrow(ResponseError);
  });

  it('sends no claim once the caller stops it after the challenge', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const server = fakeServer(() => json(500, { error: 'internal' }));
    let challenged = false;
    const proceed = () => {
      challenged = server.issued() > 0;
      return false;
    };
    await expect(server.api.claim(randomId(), readKey, { proceed })).rejects.toThrow(StoppedError);
    expect(challenged).toBe(true);
    expect(server.seen.map((request) => new URL(request.url).pathname)).toEqual(['/api/v1/challenge']);
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
      if (path.endsWith('/claim') || path.endsWith('/blob')) {
        return new Response(object, { status: 200, headers: { 'X-Nylzo-Lease': 'l', 'X-Nylzo-Policy': policy } });
      }
      if (path === '/api/v1/abuse') {
        return json(202, { status: 'accepted' });
      }
      return new Response(null, { status: 204 });
    });
    const id = randomId();
    await server.api.instance();
    await server.api.create({
      id,
      idToken: 't',
      readKey: readKey.publicKey,
      manageKey: readKey.publicKey,
      kdfId: 0,
      opensMax: 1,
      ttlSeconds: 3600,
      object,
    });
    await server.api.claim(id, readKey);
    await server.api.blob(id, readKey, 'l');
    await server.api.ack(id, readKey, 'l');
    await server.api.burn(id, readKey);
    await server.api.report(id, readKey, abuseReport('spam'));
    expect(new Set(server.seen.map((request) => new URL(request.url).pathname)).size).toBe(8);
    expect(
      server.seen.map((request) => ({
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
      () => new Response(body, { status: 200, headers: { 'X-Nylzo-Lease': 'l', 'X-Nylzo-Policy': policy } }),
    );
    await expect(server.api.claim(randomId(), readKey)).rejects.toThrow(ResponseError);
    expect(sent).toBeLessThanOrEqual(MAX_OBJECT_BYTES + 2 * chunk.length);
  });

  it.each([
    ['no', undefined],
    ['an exact', CHUNKED_OBJECT.length],
    ['a wrong', 1000],
  ])('reads an object in chunks with %s Content-Length', async (_label, length) => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const headers: Record<string, string> = {
      'X-Nylzo-Policy': policy,
      ...(length !== undefined && { 'Content-Length': String(length) }),
    };
    const progress: [number, number | undefined][] = [];
    const server = fakeServer(() => new Response(chunkedStream(CHUNKED_OBJECT), { status: 200, headers }));
    const envelope = await server.api.blob(randomId(), readKey, 'l', (received, total) => {
      progress.push([received, total]);
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
