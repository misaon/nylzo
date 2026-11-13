import {
  encodeBase64url,
  HEADER_LENGTH,
  objectHash,
  parseLink,
  PowTimeoutError,
  solvePow,
  TooLargeError,
} from '@nylzo/core';
import { stretchScrypt } from '@nylzo/core/scrypt';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import { fakeApi } from '../../shared/api/fake-api.ts';
import type { FakeApi } from '../../shared/api/fake-api.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import { CreateError, createMessage, passphraseIsWeak, POW_BUDGET_MS, validateCreate } from './create-message.ts';
import type { CreateDependencies, CreateInput } from './create-message.ts';

type Stretch = CreateDependencies['stretch'];

const stretch: Stretch = (message, onProgress) => Promise.resolve(stretchScrypt(message, onProgress));

class ImpostorError extends Error {
  override readonly name = 'KdfWorkerError';
}

interface PowCall {
  readonly challenge: string;
  readonly difficulty: number;
  readonly budgetMs: number;
}

function createDependencies(api: FakeApi, solve?: (call: PowCall, id: Uint8Array<ArrayBuffer>) => Promise<string>) {
  const powCalls: PowCall[] = [];
  const dependencies: CreateDependencies = {
    api,
    origin: 'https://nylzo.test',
    stretch,
    solvePow: async (challenge, id, difficulty, budgetMs) => {
      const call = { challenge, difficulty, budgetMs };
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
  it('W2-06, F6: never compresses an attachment, which only a sender of the CLI may choose', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(256 * 1024).fill(65);
    const file = { name: 'rows.csv', mime: 'text/csv', bytes };
    await createMessage(input({ file }), createDependencies(api));
    const [message] = api.stored.values();
    expect(message?.request.object.length).toBe(327_680);
  });

  it('creates a message whose text or file name holds a lone surrogate instead of failing', async () => {
    const api = fakeApi();
    const file = { name: 'a\uD800.txt', mime: 'text/plain', bytes: new Uint8Array([1]) };
    const created = await createMessage(input({ text: 'x\uDC00', file }), createDependencies(api));
    expect(created.link).toMatch(/\/r#3\./u);
  });

  it('F1, F7: pins the object in the link and registers with OPAQUE also without a passphrase', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const [message] = api.stored.values();
    const link = parseLink(created.link);
    expect(link.objectHash).toEqual(await objectHash(message?.request.object ?? new Uint8Array()));
    expect(message?.request.registration).toHaveLength(192);
    expect(message?.request.object).toHaveLength(65_536);
  });

  it('fetches a new id when the token expired, the id is taken or its share came from an earlier seed', async () => {
    const api = fakeApi({ failCreate: [new ApiError(403, 'id_token'), new ApiError(409, 'share_lost')] });
    const created = await createMessage(input(), createDependencies(api));
    expect(api.calls).toEqual(['instance+id', 'create', 'instance+id', 'create', 'instance+id', 'create']);
    expect(created.link).toContain(`/r#3.0.${encodeBase64url(api.issuedIds[2] ?? new Uint8Array(16))}.`);
  });

  it('solves a challenge fetched after the key derivation and stops near the window', async () => {
    const api = fakeApi({ pow: { required: true, difficulty: 2 } });
    const dependencies = createDependencies(api);
    await createMessage(input({ passphrase: 'correct horse battery staple' }), dependencies);
    expect(api.calls.filter((call) => ['instance', 'instance+id', 'solve', 'create'].includes(call))).toEqual([
      'instance+id',
      'instance',
      'solve',
      'create',
    ]);
    expect(dependencies.powCalls).toEqual([{ challenge: api.challenges[1], difficulty: 2, budgetMs: POW_BUDGET_MS }]);
    const [message] = api.stored.values();
    expect(message?.request.powToken).toMatch(new RegExp(String.raw`^${api.challenges[1]}\.[0-9a-f]{16}$`, 'u'));
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
    const ivs = Array.from(api.stored.values(), (message) => message.request.object.slice(3, HEADER_LENGTH));
    const commitments = Array.from(api.stored.values(), (message) => message.request.object.slice(HEADER_LENGTH, 47));
    expect(commitments[0]).not.toEqual(commitments[1]);
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
    expect(api.calls).toEqual(['instance+id']);
    const words = Array.from({ length: 16_000 }, (_, index) => `w${index.toString(36)}`).join(' ');
    const tooLong = createMessage(input({ passphrase: words }), createDependencies(api));
    await expect(tooLong).rejects.toHaveProperty('failure', {
      kind: 'invalid',
      problems: [{ field: 'passphrase', code: 'too_long' }],
    });
    expect(api.calls).toEqual(['instance+id']);
  });

  it('tells a failed key derivation worker and a too large message apart from other failures', async () => {
    const api = fakeApi();
    const failing = { ...createDependencies(api), stretch: () => Promise.reject(new KdfWorkerError()) };
    await expect(createMessage(input({ passphrase: 'correct horse battery staple' }), failing)).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    const renamed = { ...createDependencies(api), stretch: () => Promise.reject(new ImpostorError('worker')) };
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
    for (const [passphrase, code] of [
      ['twelve chars', 'too_short'],
      ['aaaaaaaaaaaaaaa', 'repetitive'],
      ['qwertyuiopasdfg', 'sequential'],
      ['nylzonylzo12345', 'context'],
      ['Polniypizdec0211', 'common'],
      ['tired face \u{1FAE9} is new', 'unassigned'],
    ] as const) {
      expect(validateCreate(input({ passphrase }), limits)).toEqual([{ field: 'passphrase', code }]);
    }
  });

  it('warns unless the passphrase has 16 characters and 4 words (D37)', () => {
    expect(passphraseIsWeak('twelve chars')).toBe(true);
    expect(passphraseIsWeak('sixteencharacter')).toBe(true);
    expect(passphraseIsWeak('one two three four')).toBe(false);
    expect(passphraseIsWeak('k7m2-x9pz-b4tf-hq3w')).toBe(false);
  });
});

describe('remembering the manage key (D39)', () => {
  it('returns a non-extractable manage key that signs the status of the new message', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    expect(created.manageKey.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', created.manageKey.privateKey)).rejects.toBeInstanceOf(Error);
    const status = await api.status(new Uint8Array(Buffer.from(created.id, 'base64url')), created.manageKey);
    expect(status).toMatchObject({ state: 'available', opensUsed: 0, opensMax: 1 });
  });
});
