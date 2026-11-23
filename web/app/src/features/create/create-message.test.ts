import {
  encodeBase64url,
  HEADER_LENGTH,
  MAX_MIME_BYTES,
  MAX_TEXT_BYTES,
  objectHash,
  parseLink,
  PowTimeoutError,
  solvePow,
  TooLargeError,
} from '@nylzo/core';
import { stretchScrypt } from '@nylzo/core/scrypt';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../../shared/api/api-errors.ts';
import type { Creation, Instance, IssuingInstance } from '../../shared/api/api.ts';
import { fakeApi } from '../../shared/api/fake-api.ts';
import type { FakeApi } from '../../shared/api/fake-api.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import { createReader } from '../read/reader.ts';
import type { ReadView } from '../read/reader.ts';
import { CreateError, createMessage, passphraseIsWeak, POW_BUDGET_MS, validateCreate } from './create-message.ts';
import type { CreateDependencies, CreateInput, CreateProgress } from './create-message.ts';

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
    solvePow: async ({ challenge, id, difficulty, budgetMs }) => {
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

function limitObjects(api: FakeApi, maxObjectBytes: number): void {
  const { instance } = api;
  function limited(creation: Creation): Promise<IssuingInstance>;
  function limited(): Promise<Instance>;
  async function limited(creation?: Creation): Promise<Instance> {
    const described = await (creation === undefined ? instance() : instance(creation));
    return { ...described, limits: { ...described.limits, maxObjectBytes } };
  }
  api.instance = limited;
}

async function readBack(api: FakeApi, link: string): Promise<ReadView | undefined> {
  const url = new URL(link);
  const views: ReadView[] = [];
  let fragment = url.hash;
  const session = new Map<string, string>();
  const reader = createReader(
    {
      api,
      location: { pathname: url.pathname },
      takeFragment: () => {
        const taken = fragment;
        fragment = '';
        return taken;
      },
      session: {
        getItem: (key) => session.get(key) ?? null,
        setItem: (key, value) => {
          session.set(key, value);
        },
        removeItem: (key) => {
          session.delete(key);
        },
      },
      page: { visible: () => true, prerendering: () => false },
      stretch,
      now: () => Date.now(),
    },
    (view) => {
      views.push(view);
    },
  );
  await reader.load();
  await reader.reveal();
  return views.at(-1);
}

describe('creating a message', () => {
  it('never compresses an attachment, which only a sender of the CLI may choose', async () => {
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

  it('stores a file without text and drops a media type longer than the format allows', async () => {
    const api = fakeApi();
    const file = { name: 'notes.bin', mime: `application/${'x'.repeat(MAX_MIME_BYTES)}`, bytes: new Uint8Array([7]) };
    const created = await createMessage(input({ text: '', file }), createDependencies(api));
    const revealed = await readBack(api, created.link);
    expect(revealed).toMatchObject({ view: 'revealed', file: { name: 'notes.bin', mime: '', bytes: file.bytes } });
    expect(revealed?.view === 'revealed' && revealed.text).toBeUndefined();
  });

  it('pins the object in the link and registers with OPAQUE also without a passphrase', async () => {
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

  it('reports the progress of the proof of work and skips it once the instance stops asking for one', async () => {
    const options = { pow: { required: true, difficulty: 1 } };
    const api = fakeApi(options);
    const progress: CreateProgress[] = [];
    const reporting = {
      ...createDependencies(api),
      solvePow: (_request: unknown, onProgress: (fraction: number) => void) => {
        onProgress(0.5);
        return Promise.resolve('proof');
      },
    };
    await createMessage(input(), reporting, (step) => {
      progress.push(step);
    });
    expect(progress).toContainEqual({ step: 'proving', fraction: 0.5 });
    expect([...api.stored.values()].at(-1)?.request.powToken).toBe('proof');
    const relaxing = {
      ...createDependencies(api),
      stretch: (message: Uint8Array<ArrayBuffer>, onProgress: (fraction: number) => void) => {
        options.pow.required = false;
        return stretch(message, onProgress);
      },
    };
    options.pow.required = true;
    await createMessage(input({ passphrase: 'correct horse battery staple' }), relaxing);
    expect([...api.stored.values()].at(-1)?.request.powToken).toBeUndefined();
  });

  it('refuses a message whose sealed object outgrows the instance limit', async () => {
    const api = fakeApi();
    limitObjects(api, 1000);
    await expect(createMessage(input(), createDependencies(api))).rejects.toMatchObject({
      failure: { kind: 'too_large' },
    });
    expect(api.calls).not.toContain('create');
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
    const down = fakeApi({ failCreate: [new ApiError(503, 'unavailable', { retryAfter: 5 })] });
    await expect(createMessage(input(), createDependencies(down))).rejects.toMatchObject({
      failure: { kind: 'unavailable', retryAfter: 5 },
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
    const malformed = createMessage(input({ passphrase: '\u{D800} twelve chars' }), createDependencies(api));
    await expect(malformed).rejects.toHaveProperty('failure', {
      kind: 'invalid',
      problems: [{ field: 'passphrase', code: 'malformed' }],
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
    const escapedTooLong = '"'.repeat(MAX_TEXT_BYTES);
    expect(validateCreate(input({ text: escapedTooLong }), limits)).toEqual([{ field: 'text', code: 'too_large' }]);
    expect(validateCreate(input({ opensMax: 11, ttlSeconds: 604_801 }), limits)).toEqual([
      { field: 'ttl', code: 'out_of_range' },
      { field: 'opens', code: 'out_of_range' },
    ]);
    expect(validateCreate(input({ passphrase: 'correct horse battery staple' }), limits)).toEqual([]);
    const file = { name: 'large.bin', mime: '', bytes: new Uint8Array(limits.maxFileBytes + 1) };
    expect(validateCreate(input({ file }), limits)).toEqual([{ field: 'file', code: 'too_large' }]);
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

  it('warns unless the passphrase has 16 characters and 4 words', () => {
    expect(passphraseIsWeak('')).toBe(true);
    expect(passphraseIsWeak('twelve chars')).toBe(true);
    expect(passphraseIsWeak('sixteencharacter')).toBe(true);
    expect(passphraseIsWeak('one two three four')).toBe(false);
    expect(passphraseIsWeak('k7m2-x9pz-b4tf-hq3w')).toBe(false);
  });
});

describe('remembering the manage key', () => {
  it('returns a non-extractable manage key that signs the status of the new message', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    expect(created.manageKey.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', created.manageKey.privateKey)).rejects.toBeInstanceOf(Error);
    const status = await api.status(new Uint8Array(Buffer.from(created.id, 'base64url')), created.manageKey);
    expect(status).toMatchObject({ state: 'available', opensUsed: 0, opensMax: 1 });
  });
});
