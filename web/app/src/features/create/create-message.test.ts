import { encodeBase64url, HEADER_LEN, PowTimeoutError, solvePow, TooLargeError } from '@nylzo/core';
import { scryptPassphraseKey } from '@nylzo/core/scrypt';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import { fakeApi, type FakeApi } from '../../shared/api/fake-api.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import {
  type CreateDependencies,
  CreateError,
  type CreateInput,
  createMessage,
  passphraseIsWeak,
  POW_BUDGET_MS,
  validateCreate,
} from './create-message.ts';

type Scrypt = CreateDependencies['scrypt'];

const scrypt: Scrypt = (passphrase, passphraseSalt, onProgress) =>
  Promise.resolve(scryptPassphraseKey(passphrase, passphraseSalt, onProgress));

class ImpostorError extends Error {
  override readonly name = 'KdfWorkerError';
}

interface PowCall {
  readonly challenge: string;
  readonly difficulty: number;
  readonly deadline: number;
}

function createDependencies(api: FakeApi, solve?: (call: PowCall, id: Uint8Array<ArrayBuffer>) => Promise<string>) {
  const powCalls: PowCall[] = [];
  const dependencies: CreateDependencies = {
    api,
    origin: 'https://nylzo.test',
    scrypt,
    now: () => 1000,
    solvePow: async (challenge, id, difficulty, deadline) => {
      const call = { challenge, difficulty, deadline };
      powCalls.push(call);
      api.calls.push('solve');
      return solve ? solve(call, id) : solvePow(challenge, id, difficulty);
    },
  };
  return Object.assign(dependencies, { powCalls });
}

function input(overrides: Partial<CreateInput> = {}): CreateInput {
  return { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', ...overrides };
}

describe('creating a message', () => {
  it('fetches a new id when the token expired or the id is taken', async () => {
    const api = fakeApi({ failCreate: [new ApiError(403, 'id_token'), new ApiError(409, 'conflict')] });
    const created = await createMessage(input(), createDependencies(api));
    expect(api.calls).toEqual(['instance', 'create', 'instance', 'create', 'instance', 'create']);
    expect(created.link).toContain(`/m/${encodeBase64url(api.issuedIds[2] ?? new Uint8Array(16))}#`);
  });

  it('solves a challenge fetched after the key derivation and stops near the window', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 2 } });
    const dependencies = createDependencies(api);
    await createMessage(input({ passphrase: 'correct horse battery staple' }), dependencies);
    expect(api.calls.filter((call) => ['instance', 'solve', 'create'].includes(call))).toEqual([
      'instance',
      'instance',
      'solve',
      'create',
    ]);
    expect(dependencies.powCalls).toEqual([
      { challenge: api.challenges[1], difficulty: 2, deadline: 1000 + POW_BUDGET_MS },
    ]);
    const [message] = api.stored.values();
    expect(message?.request.powToken).toMatch(new RegExp(String.raw`^${api.challenges[1]}\.[0-9a-f]{16}$`));
  });

  it('takes a fresh challenge when the first one runs out and fails as PoW after that', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 2 } });
    let timeouts = 1;
    const dependencies = createDependencies(api, (call, id) =>
      timeouts-- > 0
        ? Promise.reject(new PowTimeoutError())
        : Promise.resolve(solvePow(call.challenge, id, call.difficulty)),
    );
    await createMessage(input(), dependencies);
    expect(dependencies.powCalls.map((call) => call.challenge)).toEqual([api.challenges[1], api.challenges[2]]);
    const stuck = fakeApi({ pow: { required: true, difficulty: 2 } });
    const failing = createDependencies(stuck, () => Promise.reject(new PowTimeoutError()));
    await expect(createMessage(input(), failing)).rejects.toMatchObject({ failure: { kind: 'pow' } });
    expect(stuck.calls).not.toContain('create');
  });

  it('retries with a new id and a new challenge when the server refuses the proof of work', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 1 }, failCreate: [new ApiError(403, 'pow')] });
    const dependencies = createDependencies(api);
    await createMessage(input(), dependencies);
    expect(api.calls.filter((call) => call === 'create')).toEqual(['create', 'create']);
    expect(new Set(dependencies.powCalls.map((call) => call.challenge)).size).toBe(2);
  });

  it('seals every message with its own iv', async () => {
    const api = fakeApi();
    await createMessage(input(), createDependencies(api));
    await createMessage(input(), createDependencies(api));
    const ivs = Array.from(api.stored.values(), (message) => message.request.object.slice(3, HEADER_LEN));
    expect(ivs).toHaveLength(2);
    expect(ivs[0]).not.toEqual(ivs[1]);
  });

  it('maps rate limits and refuses invalid input before any key is made', async () => {
    const limited = fakeApi({ failCreate: [new ApiError(429, 'rate_limited', { retryAfter: 30 })] });
    await expect(createMessage(input(), createDependencies(limited))).rejects.toMatchObject({
      failure: { kind: 'rate_limited', retryAfter: 30 },
    });
    const api = fakeApi();
    const creating = createMessage(
      input({ text: '', passphrase: 'short', opensMax: 0, ttlSeconds: 30 }),
      createDependencies(api),
    );
    await expect(creating).rejects.toBeInstanceOf(CreateError);
    await expect(creating).rejects.toHaveProperty('failure', {
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
    const failing = { ...createDependencies(api), scrypt: () => Promise.reject(new KdfWorkerError()) };
    await expect(createMessage(input({ passphrase: 'correct horse battery staple' }), failing)).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    const renamed = { ...createDependencies(api), scrypt: () => Promise.reject(new ImpostorError('worker')) };
    await expect(createMessage(input({ passphrase: 'correct horse battery staple' }), renamed)).rejects.toMatchObject({
      failure: { kind: 'failed' },
    });
    api.create = () => Promise.reject(new TooLargeError());
    await expect(createMessage(input(), createDependencies(api))).rejects.toMatchObject({
      failure: { kind: 'too_large' },
    });
  });

  it('checks the limits of the instance', async () => {
    const { limits } = await fakeApi().instance();
    const tooLong = 'x'.repeat(102_401);
    expect(validateCreate(input({ text: tooLong }), limits)).toEqual([{ field: 'text', code: 'too_large' }]);
    expect(validateCreate(input({ opensMax: 11, ttlSeconds: 604_801 }), limits)).toEqual([
      { field: 'ttl', code: 'out_of_range' },
      { field: 'opens', code: 'out_of_range' },
    ]);
    expect(validateCreate(input({ passphrase: 'correct horse battery staple' }), limits)).toEqual([]);
    expect(validateCreate(input({ passphrase: '\u{D800} twelve chars' }), limits)).toEqual([
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
    const created = await createMessage(input(), createDependencies(api));
    expect(created.manageKey.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', created.manageKey.privateKey)).rejects.toThrow();
    const status = await api.status(new Uint8Array(Buffer.from(created.id, 'base64url')), created.manageKey);
    expect(status).toMatchObject({ state: 'available', opensUsed: 0, opensMax: 1 });
  });
});
