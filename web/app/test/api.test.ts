import {
  CHALLENGE_LEN,
  MAX_OBJECT_BYTES,
  encodeBase64url,
  idToString,
  requestKey,
  requestMessage,
  transferSeconds,
  utf8,
  type Purpose,
} from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import {
  ApiError,
  ResponseError,
  StoppedError,
  TRANSFER_TIMEOUT_MS,
  createApi,
  decodePolicyHeader,
} from '../src/lib/api.ts';

const randomId = () => crypto.getRandomValues(new Uint8Array(16));

type Handler = (request: Request) => Response | Promise<Response>;

function json(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } });
}

function fakeServer(handler: Handler) {
  const seen: Request[] = [];
  let issued = 0;
  const fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(new URL(String(input), 'http://nylzo.test'), init);
    seen.push(request);
    if (new URL(request.url).pathname === '/api/v1/challenge') {
      issued++;
      const challenge = new Uint8Array(CHALLENGE_LEN).fill(issued);
      return json(200, { challenge: encodeBase64url(challenge), expires_at: '2026-10-23T10:00:00Z' });
    }
    return handler(request);
  };
  return { api: createApi({ fetch }), seen, issued: () => issued };
}

function decode(s: string): Uint8Array<ArrayBuffer> {
  return Uint8Array.from(
    atob(
      s
        .replaceAll('-', '+')
        .replaceAll('_', '/')
        .padEnd(Math.ceil(s.length / 4) * 4, '='),
    ),
    (c) => c.charCodeAt(0),
  );
}

async function verifies(
  pk: string,
  challenge: string,
  signature: string,
  purpose: Purpose,
  method: string,
  path: string,
  body: Uint8Array = new Uint8Array(0),
): Promise<boolean> {
  const key = await crypto.subtle.importKey('raw', decode(pk), 'Ed25519', false, ['verify']);
  const message = await requestMessage(purpose, method, path, decode(challenge), body);
  return crypto.subtle.verify('Ed25519', key, decode(signature), message);
}

function authParts(header: string): [string, string, string] {
  const [pk = '', challenge = '', signature = ''] = header.replace(/^Nylzo-Sig /, '').split('.');
  return [pk, challenge, signature];
}

const policy = encodeBase64url(utf8(JSON.stringify({ opens_max: 1, ttl_s: 3600, kdf_id: 1, flags: 0 })));

const instanceBody = {
  limits: {
    max_file_bytes: 5242880,
    max_text_bytes: 102400,
    max_object_bytes: 5373983,
    max_ttl_s: 604800,
    default_ttl_s: 86400,
    max_opens: 10,
  },
  pow: { required: true, difficulty: 4, challenge: 'x' },
  create: { id: 'AAAAAAAAAAAAAAAAAAAAAA', id_token: 'token', expires_at: '2026-10-23T10:10:00Z' },
  versions: { api: 1, envelope: 1, fragment: 1, server: 'dev' },
};

describe('api client', () => {
  it('reads the instance and takes no passphrase profile from it', async () => {
    const good = fakeServer(() => json(200, instanceBody));
    const instance = await good.api.instance();
    expect(instance.limits).not.toHaveProperty('defaultKdfId');
    expect(instance.pow).toEqual({ required: true, difficulty: 4, challenge: 'x' });
    expect(idToString(instance.create.id)).toBe('AAAAAAAAAAAAAAAAAAAAAA');
    const legacy = fakeServer(() =>
      json(200, { ...instanceBody, limits: { ...instanceBody.limits, default_kdf_id: 0 } }),
    );
    expect((await legacy.api.instance()).limits).not.toHaveProperty('defaultKdfId');
    const bad = fakeServer(() => json(200, { ...instanceBody, limits: { ...instanceBody.limits, max_opens: 'ten' } }));
    await expect(bad.api.instance()).rejects.toThrow(ResponseError);
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
        ttl_s: 86400,
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
    const request = server.seen.at(-1);
    const path = `/api/v1/messages/${idToString(id)}`;
    expect(new URL(request?.url ?? '').pathname).toBe(path);
    expect(await verifies(...authParts(request?.headers.get('Authorization') ?? ''), 'read', 'GET', path)).toBe(true);
    expect(request?.headers.get('X-Nylzo-Verifier')).toBeNull();
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
      const request = signed[index]!;
      expect([request.method, new URL(request.url).pathname]).toEqual([method, path]);
      const parts = authParts(request.headers.get('Authorization') ?? '');
      expect(parts[0]).toBe(encodeBase64url(key.publicKey));
      expect(await verifies(...parts, purpose, method, path)).toBe(true);
      expect(await request.arrayBuffer()).toHaveProperty('byteLength', 0);
    }
  });

  it('signs an abuse report with R over its JSON body', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(7));
    const server = fakeServer(() => json(202, { status: 'accepted' }));
    await server.api.report(id, readKey, 'phishing "quoted" ✓');
    await server.api.report(id, readKey, '');
    const reports = server.seen.filter((request) => request.url.endsWith('/api/v1/abuse'));
    const bodies = [];
    for (const request of reports) {
      expect(request.method).toBe('POST');
      expect(request.headers.get('Content-Type')).toBe('application/json');
      const body = new Uint8Array(await request.arrayBuffer());
      bodies.push(JSON.parse(new TextDecoder().decode(body)));
      const parts = authParts(request.headers.get('Authorization') ?? '');
      expect(await verifies(...parts, 'read', 'POST', '/api/v1/abuse', body)).toBe(true);
      expect(await verifies(...parts, 'read', 'POST', '/api/v1/abuse')).toBe(false);
    }
    expect(bodies).toEqual([{ id: idToString(id), reason: 'phishing "quoted" ✓' }, { id: idToString(id) }]);
  });

  it('claims with R and V over the same challenge and streams the object', async () => {
    const id = randomId();
    const readKey = await requestKey(new Uint8Array(32).fill(1));
    const verifierKey = await requestKey(new Uint8Array(32).fill(2));
    const object = new Uint8Array(70000).fill(9);
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
      onLease: (lease) => leases.push(lease),
      onProgress: (received, total) => progress.push([received, total]),
    });
    expect(leases).toEqual(['lease-token']);
    expect(claimed.lease).toBe('lease-token');
    expect(claimed.policy).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 1, flags: 0 });
    expect(claimed.object).toEqual(object);
    expect(progress.at(-1)).toEqual([object.length, object.length]);
    const request = server.seen.at(-1);
    const path = `/api/v1/messages/${idToString(id)}/claim`;
    const auth = request?.headers.get('Authorization') ?? '';
    const verifier = request?.headers.get('X-Nylzo-Verifier') ?? '';
    expect(request?.method).toBe('POST');
    const [pk, challenge, signature] = authParts(auth);
    const [verifierPk = '', verifierSignature = '', ...rest] = verifier.split('.');
    expect(rest).toEqual([]);
    expect(challenge).toBe(encodeBase64url(new Uint8Array(CHALLENGE_LEN).fill(1)));
    expect(await verifies(pk, challenge, signature, 'read', 'POST', path)).toBe(true);
    expect(await verifies(verifierPk, challenge, verifierSignature, 'verifier', 'POST', path)).toBe(true);
    expect(verifierPk).toBe(encodeBase64url(verifierKey.publicKey));
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
    const request = server.seen.at(-1);
    const header = JSON.parse(
      atob((request?.headers.get('X-Nylzo-Create') ?? '').replaceAll('-', '+').replaceAll('_', '/')),
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
    expect(request?.headers.get('X-Nylzo-Pow')).toBe('chal.0000000000000003');
    expect(request?.headers.get('Content-Type')).toBe('application/octet-stream');
    expect(new Uint8Array(await request!.arrayBuffer())).toEqual(object);
  });

  it('accepts only a canonical policy header with integer fields', () => {
    expect(decodePolicyHeader(policy)).toEqual({ opensMax: 1, ttlSeconds: 3600, kdfId: 1, flags: 0 });
    for (const bad of [
      null,
      '',
      `${policy}A`,
      `${policy}=`,
      encodeBase64url(utf8('[]')),
      encodeBase64url(utf8('{"opens_max":1}')),
    ]) {
      expect(() => decodePolicyHeader(bad)).toThrow(ResponseError);
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
        ttl_s: 86400,
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
    const error = await server.api.claim(randomId(), readKey).catch((e: unknown) => e);
    expect(error).toMatchObject({ kind: 'unavailable' });
    expect((error as ApiError).retryAfter).toBeUndefined();
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
    await server.api.report(id, readKey, 'spam');
    expect(new Set(server.seen.map((request) => new URL(request.url).pathname)).size).toBe(8);
    for (const request of server.seen) {
      expect(
        {
          redirect: request.redirect,
          referrerPolicy: request.referrerPolicy,
          credentials: request.credentials,
          cache: request.cache,
        },
        request.url,
      ).toEqual({
        redirect: 'error',
        referrerPolicy: 'no-referrer',
        credentials: 'omit',
        cache: 'no-store',
      });
    }
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

  it('reads an object in chunks with and without an exact Content-Length', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(9));
    const object = Uint8Array.from({ length: 200_000 }, (_, i) => i % 251);
    const chunked = () =>
      new ReadableStream<Uint8Array>({
        start(controller) {
          for (let offset = 0; offset < object.length; offset += 30_000) {
            controller.enqueue(object.slice(offset, offset + 30_000));
          }
          controller.close();
        },
      });
    for (const length of [undefined, object.length, 1000]) {
      const headers: Record<string, string> = {
        'X-Nylzo-Policy': policy,
        ...(length === undefined ? {} : { 'Content-Length': String(length) }),
      };
      const progress: [number, number | undefined][] = [];
      const server = fakeServer(() => new Response(chunked(), { status: 200, headers }));
      const envelope = await server.api.blob(randomId(), readKey, 'l', (received, total) =>
        progress.push([received, total]),
      );
      expect(envelope.object, String(length)).toEqual(object);
      expect(envelope.object.buffer.byteLength, String(length)).toBe(object.length);
      expect(progress.at(-1), String(length)).toEqual([object.length, length]);
    }
  });

  it('gives a transfer the time the protocol allows the largest object, plus a margin', () => {
    expect(TRANSFER_TIMEOUT_MS).toBeGreaterThan(transferSeconds(MAX_OBJECT_BYTES) * 1000);
    expect(TRANSFER_TIMEOUT_MS).toBe(360_000);
  });
});

describe('transparency', () => {
  const sha = 'a'.repeat(64);
  const report = (change: Record<string, unknown> = {}) => ({
    server: 'v1.2.3',
    operator: { name: 'Example s.r.o.', contact: 'abuse@example.org' },
    limits: {
      max_file_bytes: 5242880,
      max_text_bytes: 102400,
      max_object_bytes: 5373983,
      max_ttl_s: 604800,
      default_ttl_s: 86400,
      max_opens: 10,
    },
    rates: {
      create_per_hour: 20,
      create_per_day: 100,
      create_bytes_per_day: 50000000,
      read_per_hour: 300,
      abuse_per_hour: 5,
      coarse_multiplier: 5,
      global_daily_creates: 0,
      pow_difficulty: 4,
    },
    retention: { tombstone_s: 604800, abuse_report_s: 604800, daily_counter_s: 86400 },
    ...change,
  });
  const manifest = (change: Record<string, unknown> = {}) => ({
    server: 'v1.2.3',
    built: true,
    nonce_placeholder: '__NYLZO_CSP_NONCE__',
    pages: ['/'],
    files: [{ path: '/', sha256: sha }],
    ...change,
  });
  const serve = (body: unknown, client: unknown) =>
    fakeServer((request) =>
      json(200, new URL(request.url).pathname === '/.well-known/nylzo/client.json' ? client : body),
    ).api;

  it('reads the report and the client manifest', async () => {
    const got = await serve(report(), manifest()).transparency();
    expect(got.server).toBe('v1.2.3');
    expect(got.operator).toEqual({ name: 'Example s.r.o.', contact: 'abuse@example.org' });
    expect(got.limits.maxOpens).toBe(10);
    expect(got.rates.globalDailyCreates).toBe(0);
    expect(got.retention.tombstoneSeconds).toBe(604800);
    expect(got.files).toEqual([{ path: '/', sha256: sha }]);
  });

  it('accepts an instance without an operator', async () => {
    const { operator: _, ...withoutOperator } = report();
    expect((await serve(withoutOperator, manifest()).transparency()).operator).toBeUndefined();
  });

  it('refuses malformed details', async () => {
    for (const [body, client] of [
      [report({ server: 'v9' }), manifest()],
      [report(), manifest({ files: [{ path: '/', sha256: 'A'.repeat(64) }] })],
      [report(), manifest({ files: [{ path: 'assets/x.js', sha256: sha }] })],
      [report(), manifest({ built: 'yes' })],
      [
        report(),
        manifest({
          files: [
            { path: '/', sha256: sha },
            { path: '/', sha256: sha },
          ],
        }),
      ],
      [report({ operator: { name: 'x'.repeat(201) } }), manifest()],
      [report({ operator: { contact: 7 } }), manifest()],
      [report({ operator: 'Example' }), manifest()],
      [report({ rates: {} }), manifest()],
    ]) {
      await expect(serve(body, client).transparency()).rejects.toBeInstanceOf(ResponseError);
    }
  });
});
