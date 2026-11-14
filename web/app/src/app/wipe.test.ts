import type * as Core from '@nylzo/core';
import { KDF_SCRYPT } from '@nylzo/core';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { createMessage } from '../features/create/create-message.ts';
import type { CreateDependencies } from '../features/create/create-message.ts';
import { createReader } from '../features/read/reader.ts';
import type { ReadView } from '../features/read/reader.ts';
import { ApiError } from '../shared/api/api-errors.ts';
import { fakeApi } from '../shared/api/fake-api.ts';
import type { FakeApi, FakeOptions } from '../shared/api/fake-api.ts';
import { KdfWorkerError } from '../shared/kdf/kdf-protocol.ts';

const { captured, stretched, shareSeeds, failing } = vi.hoisted(() => ({
  captured: [] as Uint8Array[],
  stretched: [] as Uint8Array[],
  shareSeeds: [] as Uint8Array[],
  failing: { requestKey: 0 },
}));

vi.mock('@nylzo/core', async (importOriginal) => {
  const core = await importOriginal<typeof Core>();
  return {
    ...core,
    newSecret: () => keep(core.newSecret()),
    parseLink: (text: string) => {
      const link = core.parseLink(text);
      keep(link.secret);
      return link;
    },
    parseFragment: (path: string, text: string) => {
      const link = core.parseFragment(path, text);
      keep(link.secret);
      return link;
    },
    deriveLink: async (id: Uint8Array, secret: Uint8Array) => {
      const keys = await core.deriveLink(id, secret);
      keep(keys.readSeed);
      keep(keys.passphraseSalt);
      return keys;
    },
    derivePassphraseSalt: async (secret: Uint8Array) => keep(await core.derivePassphraseSalt(secret)),
    opaquePassword: (...parameters: Parameters<typeof core.opaquePassword>) => keep(core.opaquePassword(...parameters)),
    createRegistrationRequest: (...parameters: Parameters<typeof core.createRegistrationRequest>) => {
      const started = core.createRegistrationRequest(...parameters);
      keep(started.state.password);
      return started;
    },
    finalizeRegistrationRequest: async (...parameters: Parameters<typeof core.finalizeRegistrationRequest>) => {
      const registration = await core.finalizeRegistrationRequest(...parameters);
      keep(registration.exportKey);
      return registration;
    },
    generateKE1: (...parameters: Parameters<typeof core.generateKE1>) => {
      const started = core.generateKE1(...parameters);
      keep(started.login.password);
      return started;
    },
    generateKE3: async (...parameters: Parameters<typeof core.generateKE3>) => {
      const result = await core.generateKE3(...parameters);
      keep(result.exportKey);
      return result;
    },
    newShareKey: () => {
      const key = core.newShareKey();
      shareSeeds.push(key.seed);
      return key;
    },
    unwrapShare: async (key: Core.ShareKey, wrapped: Uint8Array) => keep(await core.unwrapShare(key, wrapped)),
    requestKey: async (seed: Uint8Array) => {
      if (failing.requestKey > 0) {
        failing.requestKey--;
        throw new Error('this browser has no Ed25519');
      }
      return core.requestKey(seed);
    },
  };
});

const ORIGIN = 'https://nylzo.test';
const PASSPHRASE = 'correct horse battery staple';

const wiped = (bytes: Uint8Array) => bytes.length > 0 && bytes.every((byte) => byte === 0);

function zeroed(): boolean[] {
  return captured.map((bytes) => wiped(bytes));
}

// Stands in for the scrypt worker: it keeps what it gets and what it returns, so the tests see both wiped.
const stubStretch: CreateDependencies['stretch'] = (message) => {
  stretched.push(message);
  const key = new Uint8Array(message);
  stretched.push(key);
  return Promise.resolve(key);
};

function createDependencies(api: FakeApi, stretch = stubStretch): CreateDependencies {
  return { api, origin: ORIGIN, stretch, solvePow: () => Promise.resolve('') };
}

async function created(api: FakeApi, opensMax = 1, kdfId: Core.KdfId = 0): Promise<string> {
  const passphrase = kdfId === 0 ? '' : PASSPHRASE;
  const message = await createMessage(
    { text: 'ahoj', ttlSeconds: 3600, opensMax, passphrase },
    createDependencies(api),
  );
  captured.length = 0;
  stretched.length = 0;
  return message.link;
}

function readerFor(api: FakeApi, link: string) {
  const url = new URL(link);
  const views: ReadView[] = [];
  const session = new Map<string, string>();
  const reader = createReader(
    {
      api,
      location: { pathname: url.pathname },
      takeFragment: () => {
        const fragment = url.hash;
        url.hash = '';
        return fragment;
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
      stretch: stubStretch,
      now: () => Date.now(),
    },
    (view) => {
      views.push(view);
    },
  );
  return { ...reader, last: () => views.at(-1) };
}

// The sender keeps S, salt_pw, the OPAQUE password and its copy in the registration, E, P, and R with salt_pw again.
const SENDER_SECRETS = 8;

describe('secrets are wiped after use (SEC-8, D39, D41)', () => {
  beforeEach(() => {
    captured.length = 0;
    stretched.length = 0;
    shareSeeds.length = 0;
    failing.requestKey = 0;
  });

  it('wipes S, salt_pw, the OPAQUE password, E, P and R after creating a message', async () => {
    await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '' }, createDependencies(fakeApi()));
    expect(captured).toHaveLength(SENDER_SECRETS);
    expect(zeroed().every(Boolean)).toBe(true);
  });

  it('wipes the stretched OPRF output after creating a message with a passphrase', async () => {
    await createMessage(
      { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE },
      createDependencies(fakeApi()),
    );
    expect(stretched).toHaveLength(2);
    expect(stretched.every((bytes) => bytes.length === 64 && wiped(bytes))).toBe(true);
    expect(zeroed().every(Boolean)).toBe(true);
  });

  it.each<[string, FakeOptions]>([
    ['the upload fails', { failCreate: [new ApiError(500, 'internal')] }],
    [
      'a retry with a new id fails too',
      { failCreate: [new ApiError(409, 'conflict'), new ApiError(503, 'unavailable', { retryAfter: 5 })] },
    ],
  ])('wipes every secret of every attempt when %s', async (_scenario, options) => {
    const api = fakeApi(options);
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, createDependencies(api)),
    ).rejects.toBeInstanceOf(Error);
    expect(captured.length).toBe(SENDER_SECRETS * api.calls.filter((call) => call === 'create').length);
    expect(zeroed().every(Boolean)).toBe(true);
    expect(stretched.length).toBeGreaterThan(0);
    expect(stretched.every((bytes) => wiped(bytes))).toBe(true);
  });

  it.each<[string, FakeOptions]>([
    ['the message is created and read', {}],
    ['the upload fails', { failCreate: [new ApiError(500, 'internal')] }],
    ['the server withholds the share', { withholdShare: true }],
    ['the server changes the policy', { servedOpensMax: 5 }],
  ])('wipes the seed of every share key when %s', async (_scenario, options) => {
    const api = fakeApi(options);
    try {
      const reader = readerFor(api, await created(api, 2));
      await reader.load();
      await reader.reveal();
    } catch {}
    expect(shareSeeds.length).toBeGreaterThan(0);
    expect(shareSeeds.every((seed) => seed.length === 32 && wiped(seed))).toBe(true);
  });

  it('wipes the seed of the share key when the instance limits refuse the input', async () => {
    const input = { text: 'a'.repeat(102_401), ttlSeconds: 3600, opensMax: 1, passphrase: '' };
    const dependencies = createDependencies(fakeApi());
    await expect(createMessage(input, dependencies)).rejects.toBeInstanceOf(Error);
    expect(shareSeeds).toHaveLength(1);
    expect(shareSeeds.every((seed) => wiped(seed))).toBe(true);
  });

  it('wipes S, salt_pw, the OPAQUE password and E when the stretching fails', async () => {
    const dependencies = createDependencies(fakeApi(), () => Promise.reject(new KdfWorkerError()));
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, dependencies),
    ).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    expect(captured).toHaveLength(5);
    expect(zeroed()).toEqual([true, true, true, true, true]);
  });

  it('keeps only the OPAQUE password of a shown message while its remaining opens can be deleted', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 2));
    await reader.load();
    await reader.reveal();
    const password = captured[3];
    expect(captured).toHaveLength(7);
    expect(captured.filter((bytes) => bytes !== password).every((bytes) => wiped(bytes))).toBe(true);
    expect(password && wiped(password)).toBe(false);
    reader.clear();
    expect(zeroed().every(Boolean)).toBe(true);
  });

  it('wipes P and the stretched OPRF output once the reader shows the message', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()?.view).toBe('revealed');
    expect(zeroed().every(Boolean)).toBe(true);
    expect(stretched).toHaveLength(2);
    expect(stretched.every((bytes) => wiped(bytes))).toBe(true);
  });

  it('wipes S, R and salt_pw when the page cannot make the read key', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api));
    failing.requestKey = 1;
    await reader.load();
    expect(reader.last()).toEqual({ view: 'failed' });
    expect(captured).toHaveLength(3);
    expect(zeroed()).toEqual([true, true, true]);
  });

  it('wipes the OPAQUE password and P when the claim fails', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    api.claim = async (...parameters) => {
      await parameters[2].unlock();
      throw new ApiError(500, 'internal');
    };
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()).toEqual({ view: 'failed' });
    expect(captured).toHaveLength(6);
    expect(zeroed()).toEqual([true, true, true, true, true, true]);
    expect(stretched.every((bytes) => wiped(bytes))).toBe(true);
  });

  it('wipes the OPAQUE password after a wrong passphrase while it keeps S and salt_pw for the next try', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal('a wrong passphrase here');
    expect(reader.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong' } });
    const [secret, readSeed, passphraseSalt, password, loginPassword] = captured;
    expect(captured).toHaveLength(5);
    expect([secret, passphraseSalt].map((bytes) => bytes && wiped(bytes))).toEqual([false, false]);
    expect([readSeed, password, loginPassword].map((bytes) => bytes && wiped(bytes))).toEqual([true, true, true]);
    expect(stretched.every((bytes) => wiped(bytes))).toBe(true);
  });
});

function keep<Bytes extends Uint8Array>(bytes: Bytes): Bytes {
  captured.push(bytes);
  return bytes;
}
