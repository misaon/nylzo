import { CHALLENGE_LENGTH, encodeBase64url, newRequestKey, PURPOSE_ADMIN, requestMessage } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import type { Json } from '../../shared/api/responses.ts';
import { SessionEndedError } from './problems.ts';
import { CHALLENGE_FRESH_MS, SignedChannel } from './signed-channel.ts';
import type { Transport } from './transport.ts';

interface Sent {
  readonly method: string;
  readonly path: string;
  readonly authorization: string | undefined;
}

function challengeText(fill: number): string {
  return encodeBase64url(new Uint8Array(CHALLENGE_LENGTH).fill(fill));
}

function fakeServer(answer: (sent: Sent, index: number) => Promise<Json> | Json = () => ({})) {
  const sent: Sent[] = [];
  let open = 0;
  let mostOpen = 0;
  let issued = 100;
  const transport: Transport = {
    async send(method, path, options = {}) {
      const record = { method, path, authorization: options.headers?.['Authorization'] };
      sent.push(record);
      if (path === '/api/v1/challenge') {
        issued++;
        return { challenge: challengeText(issued) };
      }
      open++;
      mostOpen = Math.max(mostOpen, open);
      try {
        await Promise.resolve();
        return await answer(record, sent.length - 1);
      } finally {
        open--;
      }
    },
  };
  return { transport, sent, mostOpen: () => mostOpen };
}

function signedChallenge(sent: Sent | undefined): string {
  return sent?.authorization?.split(' ')[1]?.split('.')[1] ?? '';
}

describe('SignedChannel', () => {
  it('sends one signed request at a time and signs each over the challenge the last answer carried', async () => {
    let next = 1;
    const server = fakeServer(() => ({ challenge: challengeText(next++) }));
    const key = await newRequestKey();
    const channel = new SignedChannel({ key, transport: server.transport, now: () => 0, challenge: challengeText(0) });

    await Promise.all([
      channel.request('GET', '/api/v1/admin/live'),
      channel.request('GET', '/api/v1/admin/overview?range=7d'),
      channel.request('DELETE', '/api/v1/admin/session'),
    ]);

    expect(server.mostOpen()).toBe(1);
    expect(server.sent.map(({ method, path }) => `${method} ${path}`)).toEqual([
      'GET /api/v1/admin/live',
      'GET /api/v1/admin/overview?range=7d',
      'DELETE /api/v1/admin/session',
    ]);
    expect(server.sent.map((sent) => signedChallenge(sent))).toEqual([0, 1, 2].map((fill) => challengeText(fill)));
  });

  it('signs the method and the path with the query under the admin purpose', async () => {
    const server = fakeServer();
    const key = await newRequestKey();
    const channel = new SignedChannel({ key, transport: server.transport, now: () => 0, challenge: challengeText(7) });

    await channel.request('GET', '/api/v1/admin/overview?range=30d');

    const signature = (server.sent[0]?.authorization?.split(' ')[1] ?? '').split('.')[2] ?? '';
    const message = await requestMessage({
      purpose: PURPOSE_ADMIN,
      method: 'GET',
      path: '/api/v1/admin/overview?range=30d',
      challenge: new Uint8Array(CHALLENGE_LENGTH).fill(7),
    });
    const verifier = await crypto.subtle.importKey('raw', key.publicKey, { name: 'Ed25519' }, false, ['verify']);
    const valid = await crypto.subtle.verify(
      'Ed25519',
      verifier,
      new Uint8Array(Buffer.from(signature, 'base64url')),
      message,
    );
    expect(valid).toBe(true);
  });

  it('fetches a challenge when it holds none, when the last one is stale, and after a failed request', async () => {
    let clock = 0;
    const server = fakeServer((_, index) => {
      if (index === 3) {
        throw new ApiError(503, 'unavailable');
      }
      return index === 1 ? { challenge: challengeText(9) } : {};
    });
    const channel = new SignedChannel({ key: await newRequestKey(), transport: server.transport, now: () => clock });

    await channel.request('GET', '/api/v1/admin/live');
    clock = CHALLENGE_FRESH_MS;
    await expect(channel.request('GET', '/api/v1/admin/live')).rejects.toThrow(ApiError);
    await channel.request('GET', '/api/v1/admin/live');

    expect(server.sent.map(({ path }) => path)).toEqual([
      '/api/v1/challenge',
      '/api/v1/admin/live',
      '/api/v1/challenge',
      '/api/v1/admin/live',
      '/api/v1/challenge',
      '/api/v1/admin/live',
    ]);
  });

  it('asks once more over a fresh challenge when a kept one is refused, and keeps the session when that works', async () => {
    const server = fakeServer((_, index) => {
      if (index === 0) {
        throw new ApiError(401, 'unexpected');
      }
      return {};
    });
    const channel = new SignedChannel({
      key: await newRequestKey(),
      transport: server.transport,
      now: () => 0,
      challenge: challengeText(1),
    });

    await expect(channel.request('GET', '/api/v1/admin/live')).resolves.toEqual({});
    expect(channel.ended).toBe(false);
    expect(server.sent.map((sent) => sent.path)).toEqual([
      '/api/v1/admin/live',
      '/api/v1/challenge',
      '/api/v1/admin/live',
    ]);
    expect(signedChallenge(server.sent[2])).toBe(challengeText(101));
  });

  it('passes on any other refusal over a kept challenge without asking again or ending the session', async () => {
    const server = fakeServer(() => {
      throw new ApiError(503, 'unavailable');
    });
    const channel = new SignedChannel({
      key: await newRequestKey(),
      transport: server.transport,
      now: () => 0,
      challenge: challengeText(1),
    });

    await expect(channel.request('GET', '/api/v1/admin/live')).rejects.toThrow(ApiError);
    expect(channel.ended).toBe(false);
    expect(server.sent.map((sent) => sent.path)).toEqual(['/api/v1/admin/live']);
  });

  it('ignores a malformed challenge and fetches a fresh one instead', async () => {
    const server = fakeServer();
    const channel = new SignedChannel({
      key: await newRequestKey(),
      transport: server.transport,
      now: () => 0,
      challenge: 'not-a-challenge',
    });

    await channel.request('GET', '/api/v1/admin/live');
    expect(server.sent.map((sent) => sent.path)).toEqual(['/api/v1/challenge', '/api/v1/admin/live']);
    expect(signedChallenge(server.sent[1])).toBe(challengeText(101));
  });

  it('ends the session on 401 over a fresh challenge, refuses every later request and lets a queued one fail too', async () => {
    const server = fakeServer(() => {
      throw new ApiError(401, 'unexpected');
    });
    const channel = new SignedChannel({
      key: await newRequestKey(),
      transport: server.transport,
      now: () => 0,
      challenge: challengeText(1),
    });

    const first = channel.request('GET', '/api/v1/admin/live');
    const queued = channel.request('GET', '/api/v1/admin/overview?range=24h');

    await expect(first).rejects.toThrow(SessionEndedError);
    await expect(queued).rejects.toThrow(SessionEndedError);
    expect(channel.ended).toBe(true);
    expect(server.sent).toHaveLength(3);
  });
});
