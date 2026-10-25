import { HEADER_LEN, PowTimeoutError, TooLargeError, encodeBase64url, solvePow } from '@nylzo/core';
import { scryptPassphraseKey } from '@nylzo/core/scrypt';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../../../shared/api/api-errors.ts';
import { fakeApi, type FakeApi } from '../../../shared/api/fake-api.ts';
import { KdfWorkerError } from '../../../shared/kdf/kdf-protocol.ts';
import {
  CreateError,
  POW_BUDGET_MS,
  createMessage,
  passphraseIsWeak,
  validateCreate,
  type CreateDeps,
  type CreateInput,
} from './create-message.ts';

type Scrypt = CreateDeps['scrypt'];

const scrypt: Scrypt = async (passphrase, saltPw, onProgress) => scryptPassphraseKey(passphrase, saltPw, onProgress);

interface PowCall {
  readonly challenge: string;
  readonly difficulty: number;
  readonly deadline: number;
}

function createDeps(api: FakeApi, solve?: (call: PowCall, id: Uint8Array<ArrayBuffer>) => Promise<string>) {
  const powCalls: PowCall[] = [];
  const deps: CreateDeps = {
    api,
    origin: 'https://nylzo.test',
    scrypt,
    now: () => 1_000,
    solvePow: async (challenge, id, difficulty, deadline) => {
      const call = { challenge, difficulty, deadline };
      powCalls.push(call);
      api.calls.push('solve');
      return solve ? solve(call, id) : solvePow(challenge, id, difficulty);
    },
  };
  return Object.assign(deps, { powCalls });
}

function input(overrides: Partial<CreateInput> = {}): CreateInput {
  return { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', ...overrides };
}

describe('creating a message', () => {
  it('fetches a new id when the token expired or the id is taken', async () => {
    const api = fakeApi({ failCreate: [new ApiError(403, 'id_token'), new ApiError(409, 'conflict')] });
    const created = await createMessage(input(), createDeps(api));
    expect(api.calls).toEqual(['instance', 'create', 'instance', 'create', 'instance', 'create']);
    expect(created.link).toContain(`/m/${encodeBase64url(api.issuedIds[2] ?? new Uint8Array(16))}#`);
  });

  it('solves a challenge fetched after the key derivation and stops near the window', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 2 } });
    const deps = createDeps(api);
    await createMessage(input({ passphrase: 'correct horse battery staple' }), deps);
    expect(api.calls.filter((c) => c === 'instance' || c === 'solve' || c === 'create')).toEqual([
      'instance',
      'instance',
      'solve',
      'create',
    ]);
    expect(deps.powCalls).toEqual([{ challenge: api.challenges[1], difficulty: 2, deadline: 1_000 + POW_BUDGET_MS }]);
    const [message] = api.stored.values();
    expect(message?.request.powToken).toMatch(new RegExp(`^${api.challenges[1]}\\.[0-9a-f]{16}$`));
  });

  it('takes a fresh challenge when the first one runs out and fails as PoW after that', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 2 } });
    let timeouts = 1;
    const deps = createDeps(api, async (call, id) => {
      if (timeouts-- > 0) {
        throw new PowTimeoutError();
      }
      return solvePow(call.challenge, id, call.difficulty);
    });
    await createMessage(input(), deps);
    expect(deps.powCalls.map((c) => c.challenge)).toEqual([api.challenges[1], api.challenges[2]]);
    const stuck = fakeApi({ pow: { required: true, difficulty: 2 } });
    const failing = createDeps(stuck, async () => {
      throw new PowTimeoutError();
    });
    await expect(createMessage(input(), failing)).rejects.toMatchObject({ failure: { kind: 'pow' } });
    expect(stuck.calls).not.toContain('create');
  });

  it('retries with a new id and a new challenge when the server refuses the proof of work', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 1 }, failCreate: [new ApiError(403, 'pow')] });
    const deps = createDeps(api);
    await createMessage(input(), deps);
    expect(api.calls.filter((c) => c === 'create')).toEqual(['create', 'create']);
    expect(new Set(deps.powCalls.map((c) => c.challenge)).size).toBe(2);
  });

  it('seals every message with its own iv', async () => {
    const api = fakeApi();
    await createMessage(input(), createDeps(api));
    await createMessage(input(), createDeps(api));
    const ivs = [...api.stored.values()].map((message) => message.request.object.slice(3, HEADER_LEN));
    expect(ivs).toHaveLength(2);
    expect(ivs[0]).not.toEqual(ivs[1]);
  });

  it('maps rate limits and refuses invalid input before any key is made', async () => {
    const limited = fakeApi({ failCreate: [new ApiError(429, 'rate_limited', { retryAfter: 30 })] });
    await expect(createMessage(input(), createDeps(limited))).rejects.toMatchObject({
      failure: { kind: 'rate_limited', retryAfter: 30 },
    });
    const api = fakeApi();
    const error = await createMessage(
      input({ text: '', passphrase: 'short', opensMax: 0, ttlSeconds: 30 }),
      createDeps(api),
    ).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(CreateError);
    expect((error as CreateError).failure).toEqual({
      kind: 'invalid',
      problems: [
        { field: 'content', code: 'missing' },
        { field: 'ttl', code: 'out_of_range' },
        { field: 'opens', code: 'out_of_range' },
        { field: 'passphrase', code: 'too_short' },
      ],
    });
    expect(api.calls).toEqual(['instance']);
  });

  it('tells a failed key derivation worker and a too large message apart from other failures', async () => {
    const api = fakeApi();
    const failing = { ...createDeps(api), scrypt: async () => Promise.reject(new KdfWorkerError()) };
    await expect(createMessage(input({ passphrase: 'correct horse battery staple' }), failing)).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    const renamed = {
      ...createDeps(api),
      scrypt: async () => Promise.reject(Object.assign(new Error('worker'), { name: 'KdfWorkerError' })),
    };
    await expect(createMessage(input({ passphrase: 'correct horse battery staple' }), renamed)).rejects.toMatchObject({
      failure: { kind: 'failed' },
    });
    api.create = async () => Promise.reject(new TooLargeError());
    await expect(createMessage(input(), createDeps(api))).rejects.toMatchObject({ failure: { kind: 'too_large' } });
  });

  it('checks the limits of the instance', async () => {
    const limits = (await fakeApi().instance()).limits;
    expect(validateCreate(input({ text: 'x'.repeat(102401) }), limits)).toEqual([{ field: 'text', code: 'too_large' }]);
    expect(validateCreate(input({ opensMax: 11, ttlSeconds: 604801 }), limits)).toEqual([
      { field: 'ttl', code: 'out_of_range' },
      { field: 'opens', code: 'out_of_range' },
    ]);
    expect(validateCreate(input({ passphrase: 'correct horse battery staple' }), limits)).toEqual([]);
    expect(validateCreate(input({ passphrase: '\ud800 twelve chars' }), limits)).toEqual([
      { field: 'passphrase', code: 'malformed' },
    ]);
  });

  it('warns unless the passphrase has 16 characters and 4 words (D37)', () => {
    expect(passphraseIsWeak('twelve chars')).toBe(true);
    expect(passphraseIsWeak('sixteencharacter')).toBe(true);
    expect(passphraseIsWeak('one two three four')).toBe(false);
  });
});

describe('remembering the manage key (D39)', () => {
  it('returns a non-extractable manage key that signs the status of the new message', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    expect(created.manageKey.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', created.manageKey.privateKey)).rejects.toThrow();
    const status = await api.status(new Uint8Array(Buffer.from(created.id, 'base64url')), created.manageKey);
    expect(status).toMatchObject({ state: 'available', opensUsed: 0, opensMax: 1 });
  });
});
