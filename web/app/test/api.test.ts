import {
  CHALLENGE_LEN,
  encodeBase64url,
  idToString,
  requestKey,
  requestMessage,
  utf8,
  type Purpose,
} from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ApiError, ResponseError, createApi, decodePolicyHeader } from '../src/lib/api.ts';

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
  return Uint8Array.from(atob(s.replaceAll('-', '+').replaceAll('_', '/').padEnd(Math.ceil(s.length / 4) * 4, '=')), (c) => c.charCodeAt(0));
}

async function verifies(pk: string, challenge: string, signature: string, purpose: Purpose, method: string, path: string): Promise<boolean> {
  const key = await crypto.subtle.importKey('raw', decode(pk), 'Ed25519', false, ['verify']);
  const message = await requestMessage(purpose, method, path, decode(challenge), new Uint8Array(0));
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
    default_kdf_id: 1,
  },
  pow: { required: true, difficulty: 4, challenge: 'x' },
  create: { id: 'AAAAAAAAAAAAAAAAAAAAAA', id_token: 'token', expires_at: '2026-10-23T10:10:00Z' },
  versions: { api: 1, envelope: 1, fragment: 1, server: 'dev' },
};

describe('api client', () => {
  it('reads the instance and refuses an unknown default profile', async () => {
    const good = fakeServer(() => json(200, instanceBody));
    const instance = await good.api.instance();
    expect(instance.limits.defaultKdfId).toBe(1);
    expect(instance.pow).toEqual({ required: true, difficulty: 4, challenge: 'x' });
    expect(idToString(instance.create.id)).toBe('AAAAAAAAAAAAAAAAAAAAAA');
    const bad = fakeServer(() => json(200, { ...instanceBody, limits: { ...instanceBody.limits, default_kdf_id: 7 } }));
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
    expect(metadata).toMatchObject({ state: 'available', opensRemaining: 1, kdfId: 0, expiresAt: '2026-10-24T09:00:00Z' });
    const request = server.seen.at(-1);
    const path = `/api/v1/messages/${idToString(id)}`;
    expect(new URL(request?.url ?? '').pathname).toBe(path);
    expect(await verifies(...authParts(request?.headers.get('Authorization') ?? ''), 'read', 'GET', path)).toBe(true);
    expect(request?.headers.get('X-Nylzo-Verifier')).toBeNull();
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
          headers: { 'X-Nylzo-Lease': 'lease-token', 'X-Nylzo-Policy': policy, 'Content-Length': String(object.length) },
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
    const flaky = fakeServer(() => (++calls === 1 ? json(404, { error: 'not_found' }) : new Response(null, { status: 204 })));
    await flaky.api.ack(randomId(), readKey, 'lease');
    expect(flaky.issued()).toBe(2);
    const gone = fakeServer(() => json(404, { error: 'not_found' }));
    await expect(gone.api.ack(randomId(), readKey, 'lease')).rejects.toMatchObject({ kind: 'not_found', status: 404 });
    expect(gone.issued()).toBe(2);
  });

  it('maps errors from the server', async () => {
    const readKey = await requestKey(new Uint8Array(32).fill(4));
    const cases: [Response, Partial<ApiError>][] = [
      [json(403, { error: 'passphrase', retry_after: 60, attempts_left: 0 }), { kind: 'passphrase', retryAfter: 60, attemptsLeft: 0 }],
      [json(410, { error: 'gone', state: 'opened' }), { kind: 'gone', state: 'opened' }],
      [json(429, { error: 'rate_limited', retry_after: 5 }, { 'Retry-After': '5' }), { kind: 'rate_limited', retryAfter: 5 }],
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
    const keys = { id: randomId(), idToken: 'token', readKey: new Uint8Array(32).fill(1), manageKey: new Uint8Array(32).fill(3) };
    await server.api.create({ ...keys, kdfId: 0, opensMax: 1, ttlSeconds: 3600, object, powToken: 'chal.0000000000000003' });
    const request = server.seen.at(-1);
    const header = JSON.parse(atob((request?.headers.get('X-Nylzo-Create') ?? '').replaceAll('-', '+').replaceAll('_', '/')));
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
    for (const bad of [null, '', `${policy}A`, `${policy}=`, encodeBase64url(utf8('[]')), encodeBase64url(utf8('{"opens_max":1}'))]) {
      expect(() => decodePolicyHeader(bad)).toThrow(ResponseError);
    }
  });
});
