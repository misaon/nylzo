import { CHALLENGE_LENGTH, encodeBase64url, wipeRequestKey } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError, ResponseError } from '../../shared/api/api-errors.ts';
import { isObject } from '../../shared/api/responses.ts';
import type { Json } from '../../shared/api/responses.ts';
import { TIMELINE_METRICS } from '../api/types.ts';
import { createAdminClient } from './client.ts';
import { createPasskey, usePasskey } from './passkey.ts';
import { SessionEndedError } from './problems.ts';

vi.mock(import('@nylzo/core'), (original) =>
  original().then((actual) => ({
    ...actual,
    wipeRequestKey: vi.fn<typeof actual.wipeRequestKey>(actual.wipeRequestKey),
  })),
);
vi.mock(import('./passkey.ts'), () => ({
  passkeysSupported: vi.fn<typeof import('./passkey.ts').passkeysSupported>(),
  createPasskey: vi.fn<typeof import('./passkey.ts').createPasskey>(),
  usePasskey: vi.fn<typeof import('./passkey.ts').usePasskey>(),
}));

const REGISTRATION = {
  credential_id: 'credential',
  client_data: 'client',
  authenticator_data: 'authenticator',
  public_key: 'public',
  algorithm: -8,
};
const ASSERTION = { credential_id: 'credential', client_data: 'client', authenticator_data: 'data', signature: 'sig' };

interface Received {
  readonly method: string;
  readonly path: string;
  readonly body: Json | undefined;
  readonly authorization: string | null;
}

type Answer = Json | ((received: Received) => Response);

function challengeText(fill: number): string {
  return encodeBase64url(new Uint8Array(CHALLENGE_LENGTH).fill(fill));
}

function overviewJson(): Json {
  const metrics = Object.fromEntries(TIMELINE_METRICS.map((metric) => [metric, 1]));
  return {
    range: '7d',
    from: 1_700_000_000,
    resolution_s: 86_400,
    series: Object.fromEntries(TIMELINE_METRICS.map((metric) => [metric, [1]])),
    totals: metrics,
    previous: null,
    countries: [],
    pages: [],
    robots: [],
    stored: 0,
    storage: { free_bytes: 1, total_bytes: 2, reserve_bytes: 0, database_bytes: 0 },
    security: { passkeys: 1, last_sign_in: 0, session_expires_at: 1_700_000_900 },
    source: { countries: '', attribution: '', attribution_url: '' },
    version: '0.15.0',
  };
}

function liveJson(at: number): Json {
  return {
    online: 1,
    people: [],
    robots: [],
    session_expires_at: 1_700_000_900,
    usage_supported: true,
    usage: [{ at, processor: 0, load: 0, memory_used: 0, memory_total: 0, process_bytes: 0 }],
    refused: [],
    refused_version: 3,
  };
}

function fakeServer(answers: Readonly<Record<string, Answer>>) {
  const received: Received[] = [];
  let issued = 0;
  const fetchResource = (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
    if (typeof input !== 'string') {
      return Promise.reject(new TypeError('the admin client sends paths only'));
    }
    const url = new URL(input, 'https://nylzo.example');
    const method = init.method ?? 'GET';
    const body: unknown = typeof init.body === 'string' ? JSON.parse(init.body) : undefined;
    const request: Received = {
      method,
      path: `${url.pathname}${url.search}`,
      body: isObject(body) ? body : undefined,
      authorization: new Headers(init.headers).get('Authorization'),
    };
    received.push(request);
    const answer = answers[`${method} ${url.pathname}`];
    if (typeof answer === 'function') {
      return Promise.resolve(answer(request));
    }
    issued++;
    const json = url.pathname === '/api/v1/challenge' ? {} : (answer ?? {});
    return Promise.resolve(Response.json({ challenge: challengeText(issued), ...json }));
  };
  return { received, client: createAdminClient(fetchResource) };
}

function refusal(status: number): () => Response {
  return () => Response.json({ error: 'unexpected' }, { status });
}

function signer(authorization: string | null | undefined): Record<'challenge' | 'key', string | undefined> {
  const [key, challenge] = authorization?.split(' ')[1]?.split('.') ?? [];
  return { key, challenge };
}

describe('createAdminClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(createPasskey).mockResolvedValue(REGISTRATION);
    vi.mocked(usePasskey).mockResolvedValue(ASSERTION);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('reports whether a passkey is enrolled and refuses an answer that does not say', async () => {
    await expect(fakeServer({ 'GET /api/v1/admin': { enrolled: true } }).client.enrolled()).resolves.toBe(true);
    await expect(fakeServer({ 'GET /api/v1/admin': { enrolled: false } }).client.enrolled()).resolves.toBe(false);
    await expect(fakeServer({ 'GET /api/v1/admin': { enrolled: 'yes' } }).client.enrolled()).rejects.toThrow(
      ResponseError,
    );
  });

  it('enrolls a passkey for the ceremony the token opened and sends the token with it', async () => {
    const ceremony = { public_key: { challenge: 'abc' } };
    const server = fakeServer({ 'POST /api/v1/admin/enrollment/options': ceremony });

    await server.client.enroll('token');

    expect(vi.mocked(createPasskey).mock.calls[0]?.[0]).toMatchObject(ceremony);
    expect(server.received.map(({ method, path, body }) => ({ method, path, body }))).toEqual([
      { method: 'POST', path: '/api/v1/admin/enrollment/options', body: { token: 'token' } },
      { method: 'POST', path: '/api/v1/admin/enrollment', body: { token: 'token', ...REGISTRATION } },
    ]);
  });

  it('binds the session to a new key and signs every later request with it over the last challenge', async () => {
    const server = fakeServer({
      'POST /api/v1/admin/session': { expires_at: 1_700_000_900 },
      'GET /api/v1/admin/overview': overviewJson(),
      'GET /api/v1/admin/live': liveJson(100),
    });

    const session = await server.client.signIn();
    expect(session.expiresAt).toBe(1_700_000_900);
    const [options, creation] = server.received;
    const key = options?.body?.['key'];
    expect(typeof key).toBe('string');
    expect(creation?.body).toEqual({ key, ...ASSERTION });

    await expect(session.overview('7d')).resolves.toMatchObject({ range: '7d', resolutionSeconds: 86_400 });
    await expect(session.live()).resolves.toMatchObject({ online: 1, usage: [{ at: 100 }] });
    await session.live();

    const signed = server.received.slice(2);
    expect(signed.map(({ method, path }) => `${method} ${path}`)).toEqual([
      'GET /api/v1/admin/overview?range=7d',
      'GET /api/v1/admin/live',
      'GET /api/v1/admin/live?since=100&refused=3',
    ]);
    expect(signed.map(({ authorization }) => signer(authorization))).toEqual(
      [2, 3, 4].map((issued) => ({ key, challenge: challengeText(issued) })),
    );
  });

  it('asks for a challenge when the sign-in answer carries none', async () => {
    const server = fakeServer({
      'POST /api/v1/admin/session': () => Response.json({ expires_at: 5 }),
      'GET /api/v1/admin/live': liveJson(1),
    });

    const session = await server.client.signIn();
    await session.live();

    expect(server.received.slice(2).map(({ path }) => path)).toEqual(['/api/v1/challenge', '/api/v1/admin/live']);
    expect(signer(server.received[3]?.authorization).challenge).toBe(challengeText(2));
  });

  it('signs out on the server and refuses every later request even when the sign-out fails', async () => {
    const server = fakeServer({
      'POST /api/v1/admin/session': { expires_at: 5 },
      'DELETE /api/v1/admin/session': refusal(503),
    });
    const session = await server.client.signIn();

    await expect(session.signOut()).rejects.toThrow(ApiError);
    await expect(session.live()).rejects.toThrow(SessionEndedError);

    expect(server.received.at(-1)?.method).toBe('DELETE');
    expect(server.received).toHaveLength(3);
  });

  it.each([
    ['the server refuses the passkey', { 'POST /api/v1/admin/session': refusal(401) }, ApiError],
    ['the answer carries no expiry', { 'POST /api/v1/admin/session': {} }, ResponseError],
  ])('wipes the new session key when %s', async (_, answers, failure) => {
    const server = fakeServer(answers);

    await expect(server.client.signIn()).rejects.toThrow(failure);

    expect(wipeRequestKey).toHaveBeenCalledOnce();
    const wiped = vi.mocked(wipeRequestKey).mock.calls[0]?.[0];
    expect(encodeBase64url(wiped?.publicKey ?? new Uint8Array())).toBe(server.received[0]?.body?.['key']);
  });

  it('wipes the new session key when the passkey ceremony is cancelled', async () => {
    vi.mocked(usePasskey).mockRejectedValue(new DOMException('cancelled', 'NotAllowedError'));
    const server = fakeServer({});

    await expect(server.client.signIn()).rejects.toMatchObject({ name: 'NotAllowedError' });

    expect(wipeRequestKey).toHaveBeenCalledOnce();
    expect(server.received.map(({ path }) => path)).toEqual(['/api/v1/admin/session/options']);
  });
});
