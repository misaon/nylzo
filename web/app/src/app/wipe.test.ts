import { KDF_SCRYPT, type KdfId } from '@nylzo/core';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { type CreateDependencies, createMessage } from '../features/create/create-message.ts';
import { createReader, type ReadView } from '../features/read/reader.ts';
import { ApiError } from '../shared/api/api-errors.ts';
import { fakeApi, type FakeApi, type FakeOptions } from '../shared/api/fake-api.ts';
import { KdfWorkerError } from '../shared/kdf/kdf-protocol.ts';
import { reportLink } from '../shared/report/report.ts';

const { captured, passphraseKeys, failing } = vi.hoisted(() => ({
  captured: [] as Uint8Array[],
  passphraseKeys: [] as Uint8Array[],
  failing: { requestKey: 0 },
}));

vi.mock('@nylzo/core', async (importOriginal) => {
  const core = await importOriginal<typeof import('@nylzo/core')>();
  const keep = <T extends Uint8Array>(bytes: T): T => {
    captured.push(bytes);
    return bytes;
  };
  return {
    ...core,
    newSecret: () => keep(core.newSecret()),
    parseLink: (raw: string) => {
      const link = core.parseLink(raw);
      keep(link.s);
      return link;
    },
    parseFragment: (text: string) => {
      const fragment = core.parseFragment(text);
      keep(fragment.s);
      return fragment;
    },
    deriveLink: async (id: Uint8Array, s: Uint8Array) => {
      const keys = await core.deriveLink(id, s);
      keep(keys.r);
      keep(keys.saltPw);
      return keys;
    },
    deriveContent: async (id: Uint8Array, s: Uint8Array, p: Uint8Array) => {
      const keys = await core.deriveContent(id, s, p);
      keep(keys.v);
      return keys;
    },
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
  return captured.map(wiped);
}

function scryptStub(): CreateDependencies['scrypt'] {
  return async (passphrase) => {
    const p = new Uint8Array(32).fill(passphrase === PASSPHRASE ? 7 : 9);
    passphraseKeys.push(p);
    return p;
  };
}

function createDependencies(api: FakeApi, scrypt = scryptStub()): CreateDependencies {
  return { api, origin: ORIGIN, scrypt, solvePow: async () => '' };
}

async function created(api: FakeApi, opensMax = 1, kdfId: KdfId = 0, scrypt = scryptStub()): Promise<string> {
  const passphrase = kdfId === 0 ? '' : PASSPHRASE;
  const message = await createMessage(
    { text: 'ahoj', ttlSeconds: 3600, opensMax, passphrase },
    createDependencies(api, scrypt),
  );
  captured.length = 0;
  passphraseKeys.length = 0;
  return message.link;
}

function readerFor(api: FakeApi, link: string, scrypt = scryptStub()) {
  const url = new URL(link);
  const views: ReadView[] = [];
  const reader = createReader(
    {
      api,
      location: { pathname: url.pathname, hash: url.hash },
      session: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
      page: { visible: () => true, prerendering: () => false },
      dropFragment: () => {},
      scrypt,
      now: () => Date.now(),
    },
    (view) => views.push(view),
  );
  return { ...reader, last: () => views.at(-1) };
}

describe('secrets are wiped after use (SEC-8, D39, D41)', () => {
  beforeEach(() => {
    captured.length = 0;
    passphraseKeys.length = 0;
    failing.requestKey = 0;
  });

  it('wipes S, R, salt_pw and V after creating a message', async () => {
    await createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '' }, createDependencies(fakeApi()));
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
  });

  it('wipes P after creating a message', async () => {
    await createMessage(
      { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE },
      createDependencies(fakeApi()),
    );
    expect(passphraseKeys).toHaveLength(1);
    expect(passphraseKeys.every((p) => p.length === 32 && wiped(p))).toBe(true);
  });

  it.each<[string, FakeOptions]>([
    ['the upload fails', { failCreate: [new ApiError(500, 'internal')] }],
    [
      'a retry with a new id fails too',
      { failCreate: [new ApiError(409, 'conflict'), new ApiError(503, 'unavailable', { retryAfter: 5 })] },
    ],
  ])('wipes every S, R, salt_pw, V and P when %s', async (_, options) => {
    const api = fakeApi(options);
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, createDependencies(api)),
    ).rejects.toThrow();
    expect(captured.length).toBe(4 * api.calls.filter((call) => call === 'create').length);
    expect(zeroed().every(Boolean)).toBe(true);
    expect(passphraseKeys.length).toBeGreaterThan(0);
    expect(passphraseKeys.every(wiped)).toBe(true);
  });

  it('wipes S, R and salt_pw when the key derivation fails', async () => {
    const dependencies = createDependencies(fakeApi(), async () => {
      throw new KdfWorkerError();
    });
    await expect(
      createMessage({ text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: PASSPHRASE }, dependencies),
    ).rejects.toMatchObject({
      failure: { kind: 'kdf' },
    });
    expect(captured).toHaveLength(3);
    expect(zeroed()).toEqual([true, true, true]);
  });

  it('wipes S, R and salt_pw after reporting a pasted link', async () => {
    const api = fakeApi();
    const link = await created(api);
    expect(await reportLink(api, ORIGIN, link, 'spam')).toEqual({ outcome: 'accepted' });
    expect(captured).toHaveLength(3);
    expect(zeroed()).toEqual([true, true, true]);
  });

  it('wipes S, R and salt_pw when a pasted link gives no read key', async () => {
    const api = fakeApi();
    const link = await created(api);
    failing.requestKey = 1;
    expect(await reportLink(api, ORIGIN, link, 'spam')).toEqual({ outcome: 'failed' });
    expect(captured).toHaveLength(3);
    expect(zeroed()).toEqual([true, true, true]);
    expect(api.calls).not.toContain('report');
  });

  it('wipes S when it refuses a link of another service', async () => {
    const api = fakeApi();
    const link = await created(api);
    expect(await reportLink(api, 'https://other.test', link, '')).toEqual({
      outcome: 'invalid',
      problems: ['link_foreign'],
    });
    expect(captured).toHaveLength(1);
    expect(zeroed()).toEqual([true]);
  });

  it('wipes S, R, salt_pw and V once the reader shows the message', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 2));
    await reader.load();
    await reader.reveal();
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
    expect(await reader.report('after reading')).toEqual({ outcome: 'accepted' });
  });

  it('wipes P once the reader shows the message', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()?.view).toBe('revealed');
    expect(passphraseKeys).toHaveLength(1);
    expect(passphraseKeys.every((p) => p.length === 32 && wiped(p))).toBe(true);
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
    api.claim = async () => {
      throw new ApiError(500, 'internal');
    };
    await reader.load();
    await reader.reveal(PASSPHRASE);
    expect(reader.last()).toEqual({ view: 'failed' });
    expect(captured).toHaveLength(4);
    expect(zeroed()).toEqual([true, true, true, true]);
    expect(passphraseKeys.every(wiped)).toBe(true);
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
    expect(passphraseKeys.every(wiped)).toBe(true);
  });

  it('wipes V and P after a wrong passphrase while it keeps S and salt_pw for the next try', async () => {
    const api = fakeApi();
    const reader = readerFor(api, await created(api, 1, KDF_SCRYPT));
    await reader.load();
    await reader.reveal('a wrong passphrase here');
    expect(reader.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong' } });
    const [s, r, saltPw, v] = captured;
    expect([s, saltPw].map((bytes) => bytes && wiped(bytes))).toEqual([false, false]);
    expect([r, v].map((bytes) => bytes && wiped(bytes))).toEqual([true, true]);
    expect(passphraseKeys.every(wiped)).toBe(true);
  });
});
