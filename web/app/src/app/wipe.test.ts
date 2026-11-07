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

const { captured, passphraseKeys, failing } = vi.hoisted(() => ({
  captured: [] as Uint8Array[],
  passphraseKeys: [] as Uint8Array[],
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
    deriveVerifier: async (id: Uint8Array, secret: Uint8Array, passphraseKey: Uint8Array) =>
      keep(await core.deriveVerifier(id, secret, passphraseKey)),
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

const stubScrypt: CreateDependencies['scrypt'] = (passphrase) => {
  const passphraseKey = new Uint8Array(32).fill(passphrase === PASSPHRASE ? 7 : 9);
  passphraseKeys.push(passphraseKey);
  return Promise.resolve(passphraseKey);
};

function createDependencies(api: FakeApi, scrypt = stubScrypt): CreateDependencies {
  return { api, origin: ORIGIN, scrypt, solvePow: () => Promise.resolve('') };
}

async function created(api: FakeApi, opensMax = 1, kdfId: Core.KdfId = 0): Promise<string> {
  const passphrase = kdfId === 0 ? '' : PASSPHRASE;
  const message = await createMessage(
    { text: 'ahoj', ttlSeconds: 3600, opensMax, passphrase },
    createDependencies(api),
  );
  captured.length = 0;
  passphraseKeys.length = 0;
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
      scrypt: stubScrypt,
      now: () => Date.now(),
    },
    (view) => {
      views.push(view);
    },
  );
  return { ...reader, last: () => views.at(-1) };
}

describe('secrets are wiped after use (SEC-8, D39, D41)', () => {
  beforeEach(() => {
    captured.length = 0;
    passphraseKeys.length = 0;
    failing.requestKey = 0;
  });

  it('wipes E, S, R, salt_pw and V after creating a message', async () => {
    await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '' }, createDependencies(fakeApi()));
    expect(captured).toHaveLength(5);
    expect(zeroed()).toEqual([true, true, true, true, true]);
  });

  it('wipes P after creating a message', async () => {
    await createMessage(
      { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE },
      createDependencies(fakeApi()),
    );
    expect(passphraseKeys).toHaveLength(1);
    expect(passphraseKeys.every((passphraseKey) => passphraseKey.length === 32 && wiped(passphraseKey))).toBe(true);
  });

  it.each<[string, FakeOptions]>([
    ['the upload fails', { failCreate: [new ApiError(500, 'internal')] }],
    [
      'a retry with a new id fails too',
      { failCreate: [new ApiError(409, 'conflict'), new ApiError(503, 'unavailable', { retryAfter: 5 })] },
    ],
  ])('wipes every E, S, R, salt_pw, V and P when %s', async (_scenario, options) => {
    const api = fakeApi(options);
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, createDependencies(api)),
    ).rejects.toBeInstanceOf(Error);
    expect(captured.length).toBe(5 * api.calls.filter((call) => call === 'create').length);
    expect(zeroed().every(Boolean)).toBe(true);
    expect(passphraseKeys.length).toBeGreaterThan(0);
    expect(passphraseKeys.every((bytes) => wiped(bytes))).toBe(true);
  });

  it('wipes E, S, R and salt_pw when the key derivation fails', async () => {
    const dependencies = createDependencies(fakeApi(), () => Promise.reject(new KdfWorkerError()));
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, dependencies),
    ).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
  });

  it('wipes S, R, salt_pw, V and E once the reader shows the message', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 2));
    await reader.load();
    await reader.reveal();
    expect(captured).toHaveLength(5);
    expect(zeroed()).toEqual([true, true, true, true, true]);
  });

  it('wipes P once the reader shows the message', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()?.view).toBe('revealed');
    expect(passphraseKeys).toHaveLength(1);
    expect(passphraseKeys.every((passphraseKey) => passphraseKey.length === 32 && wiped(passphraseKey))).toBe(true);
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

  it('wipes S, R, salt_pw, V and P when the claim fails', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    api.claim = () => Promise.reject(new ApiError(500, 'internal'));
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()).toEqual({ view: 'failed' });
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
    expect(passphraseKeys.every((bytes) => wiped(bytes))).toBe(true);
  });

  it('wipes S, R, salt_pw, V and P when the verifier key cannot be made', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    failing.requestKey = 1;
    await reader.reveal(PASSPHRASE);
    expect(reader.last()).toEqual({ view: 'failed' });
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
    expect(passphraseKeys).toHaveLength(1);
    expect(passphraseKeys.every((bytes) => wiped(bytes))).toBe(true);
  });

  it('wipes V and P after a wrong passphrase while it keeps S and salt_pw for the next try', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal('a wrong passphrase here');
    expect(reader.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong' } });
    const [secret, readSeed, passphraseSalt, verifierSeed] = captured;
    expect([secret, passphraseSalt].map((bytes) => bytes && wiped(bytes))).toEqual([false, false]);
    expect([readSeed, verifierSeed].map((bytes) => bytes && wiped(bytes))).toEqual([true, true]);
    expect(passphraseKeys.every((bytes) => wiped(bytes))).toBe(true);
  });
});

function keep<Bytes extends Uint8Array>(bytes: Bytes): Bytes {
  captured.push(bytes);
  return bytes;
}
