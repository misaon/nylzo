import { KDF_NONE, KDF_PBKDF2, KDF_SCRYPT, POW_CHALLENGE_LEN, derivePassphraseKey, encodeBase64url, leadingZeroBits, solvePow, type KdfId } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../src/lib/api.ts';
import { CreateError, createMessage, passphraseIsWeak, validateCreate, type CreateInput } from '../src/lib/create.ts';
import { createReader, type ReadView, type SessionStore } from '../src/lib/read.ts';
import { fakeApi, type FakeApi } from './fake-api.ts';

const scrypt = (passphrase: string, saltPw: Uint8Array<ArrayBuffer>, onProgress: (fraction: number) => void) =>
  derivePassphraseKey(KDF_SCRYPT, passphrase, saltPw, { onProgress });

const powCalls: { challenge: string; difficulty: number }[] = [];
const createDeps = (api: FakeApi) => ({
  api,
  origin: 'https://nylzo.test',
  scrypt,
  solvePow: async (challenge: string, id: Uint8Array<ArrayBuffer>, difficulty: number) => {
    powCalls.push({ challenge, difficulty });
    return solvePow(challenge, id, difficulty);
  },
});

function input(overrides: Partial<CreateInput> = {}): CreateInput {
  return { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', kdfId: KDF_NONE, ...overrides };
}

function memorySession(): SessionStore & { map: Map<string, string> } {
  const map = new Map<string, string>();
  return {
    map,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => void map.set(key, value),
    removeItem: (key) => void map.delete(key),
  };
}

function reader(api: FakeApi, link: string, overrides: { session?: SessionStore; visible?: boolean; prerendering?: boolean } = {}) {
  const url = new URL(link);
  const views: ReadView[] = [];
  let dropped = 0;
  const session = overrides.session ?? memorySession();
  const instance = createReader(
    {
      api,
      location: { pathname: url.pathname, hash: url.hash },
      session,
      page: { visible: () => overrides.visible ?? true, prerendering: () => overrides.prerendering ?? false },
      dropFragment: () => void dropped++,
      scrypt,
      now: () => Date.now(),
    },
    (view) => views.push(view),
  );
  return { ...instance, views, last: () => views.at(-1), dropped: () => dropped, session };
}

describe('create and read', () => {
  it('round-trips a text without passphrase and drops the fragment', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    expect(created.link).toMatch(/^https:\/\/nylzo\.test\/m\/[A-Za-z0-9_-]{22}#1\.0\.[A-Za-z0-9_-]{43}$/);
    const r = reader(api, created.link);
    await r.load();
    expect(r.last()).toMatchObject({ view: 'ready', resumable: false, deviceFailed: false });
    await r.reveal();
    expect(r.last()).toEqual({ view: 'revealed', text: 'ahoj', file: undefined });
    expect(api.calls).toEqual(['instance', 'create', 'metadata', 'claim', 'ack']);
    expect(r.dropped()).toBe(1);
    expect([...(r.session as ReturnType<typeof memorySession>).map.keys()]).toEqual([]);
  });

  it('round-trips a file behind a PBKDF2 passphrase after one wrong attempt', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(70000).map((_, i) => i % 251);
    const created = await createMessage(
      input({ text: '', file: { name: '../report‮.pdf', mime: 'application/pdf', bytes }, passphrase: 'tři slova a ještě čtvrté', kdfId: KDF_PBKDF2 }),
      createDeps(api),
    );
    expect(created.link).toContain('#1.2.');
    const r = reader(api, created.link);
    await r.load();
    await r.reveal('špatná passphrase tady');
    expect(r.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong', attemptsLeft: 4, locked: false } });
    await r.reveal('tři slova a ještě čtvrté');
    const last = r.last();
    expect(last?.view).toBe('revealed');
    if (last?.view === 'revealed') {
      expect(last.file?.name).toBe('_report.pdf');
      expect(last.file?.mime).toBe('application/pdf');
      expect(last.file?.bytes).toEqual(bytes);
      expect(last.text).toBeUndefined();
    }
    expect(api.calls.filter((c) => c.startsWith('claim'))).toEqual(['claim+verifier', 'claim+verifier']);
  });

  it('derives scrypt profile 1 with progress on both sides', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_SCRYPT }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal('correct horse battery staple');
    expect(r.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    const deriving = r.views.filter((v) => v.view === 'busy' && v.step === 'deriving');
    expect(deriving.length).toBeGreaterThan(2);
  });

  it('stops before any verifier when the server reports another profile (AK24)', async () => {
    const api = fakeApi({ metadataKdfId: KDF_PBKDF2 });
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_SCRYPT }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(r.last()).toEqual({ view: 'tampered' });
    await r.reveal('correct horse battery staple');
    expect(api.calls.some((c) => c.startsWith('claim'))).toBe(false);
  });

  it('reports a changed policy or ciphertext as tampered', async () => {
    for (const options of [{ servedKdfId: KDF_PBKDF2 }, { tamperObject: true }]) {
      const api = fakeApi(options);
      const created = await createMessage(input(), createDeps(api));
      const r = reader(api, created.link);
      await r.load();
      await r.reveal();
      expect(r.last()).toEqual({ view: 'tampered' });
      expect(r.dropped()).toBe(0);
    }
  });

  it('never claims from a hidden or prerendered page', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    for (const overrides of [{ visible: false }, { prerendering: true }]) {
      const r = reader(api, created.link, overrides);
      await r.load();
      await r.reveal();
      expect(r.last()?.view).toBe('ready');
    }
    expect(api.calls.filter((c) => c === 'claim')).toEqual([]);
  });

  it('resumes from the stored lease after a reload instead of spending another open', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const session = memorySession();
    const first = reader(api, created.link, { session });
    await first.load();
    const [, idText] = /\/m\/([^#]+)/.exec(created.link) ?? [];
    const stored = api.stored.get(idText ?? '');
    await api.claim(stored?.request.id ?? new Uint8Array(16), { publicKey: new Uint8Array(stored?.request.readKey ?? []), privateKey: {} as CryptoKey }, {
      onLease: (lease) => session.setItem(`nylzo:lease:${idText}`, lease),
    });
    const second = reader(api, created.link, { session });
    await second.load();
    expect(second.last()).toMatchObject({ view: 'ready', resumable: true });
    await second.reveal();
    expect(second.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    expect(api.calls.slice(-2)).toEqual(['blob', 'ack']);
    const third = reader(api, created.link);
    await third.load();
    expect(third.last()).toMatchObject({ view: 'ended', state: 'opened' });
  });

  it('tells the reader when wrong attempts locked the message for its lifetime', async () => {
    const api = fakeApi({ lockAfter: 1, expiresInSeconds: 600 });
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_PBKDF2 }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal('nesprávná passphrase');
    expect(r.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong', attemptsLeft: 0, retryAfter: 3600, locked: true } });
  });

  it('warns after a key derivation that the system ended', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_PBKDF2 }), createDeps(api));
    const session = memorySession();
    const [, idText] = /\/m\/([^#]+)/.exec(created.link) ?? [];
    session.setItem(`nylzo:kdf:${idText}`, '1');
    const r = reader(api, created.link, { session });
    await r.load();
    expect(r.last()).toMatchObject({ view: 'ready', deviceFailed: true });
    expect(session.map.size).toBe(0);
  });

  it('tells a malformed link from an unknown one', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const broken = reader(api, created.link.replace('#1.', '#2.'));
    await broken.load();
    expect(broken.last()).toEqual({ view: 'invalid' });
    const other = encodeBase64url(new Uint8Array(32).fill(5));
    const unknown = reader(api, created.link.replace(/#1\.0\..*$/, `#1.0.${other}`));
    await unknown.load();
    expect(unknown.last()).toEqual({ view: 'unknown' });
  });
});

describe('create', () => {
  it('fetches a new id when the token expired or the id is taken', async () => {
    const api = fakeApi({ failCreate: [new ApiError(403, 'id_token'), new ApiError(409, 'conflict')] });
    const created = await createMessage(input(), createDeps(api));
    expect(api.calls).toEqual(['instance', 'create', 'instance', 'create', 'instance', 'create']);
    expect(created.link).toContain(`/m/${encodeBase64url(api.issuedIds[2] ?? new Uint8Array(16))}#`);
  });

  it('solves the proof of work the instance asks for', async () => {
    const challenge = encodeBase64url(new Uint8Array(POW_CHALLENGE_LEN).fill(3));
    const api = fakeApi({ pow: { required: true, difficulty: 2, challenge } });
    powCalls.length = 0;
    await createMessage(input(), createDeps(api));
    expect(powCalls).toEqual([{ challenge, difficulty: 2 }]);
    const [message] = api.stored.values();
    expect(message?.request.powToken).toMatch(new RegExp(`^${challenge}\\.[0-9a-f]{16}$`));
    expect(leadingZeroBits(new Uint8Array([0x3f]))).toBe(2);
  });

  it('maps rate limits and refuses invalid input before any key is made', async () => {
    const limited = fakeApi({ failCreate: [new ApiError(429, 'rate_limited', 30)] });
    await expect(createMessage(input(), createDeps(limited))).rejects.toMatchObject({ failure: { kind: 'rate_limited', retryAfter: 30 } });
    const api = fakeApi();
    const error = await createMessage(input({ text: '', passphrase: 'short', kdfId: KDF_SCRYPT, opensMax: 0, ttlSeconds: 30 }), createDeps(api)).catch((e: unknown) => e);
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

  it('checks the limits of the instance', async () => {
    const limits = (await fakeApi().instance()).limits;
    expect(validateCreate(input({ text: 'x'.repeat(102401) }), limits)).toEqual([{ field: 'text', code: 'too_large' }]);
    expect(validateCreate(input({ opensMax: 11, ttlSeconds: 604801 }), limits)).toEqual([
      { field: 'ttl', code: 'out_of_range' },
      { field: 'opens', code: 'out_of_range' },
    ]);
    expect(validateCreate(input({ passphrase: 'correct horse battery staple', kdfId: KDF_NONE as KdfId }), limits)).toEqual([
      { field: 'passphrase', code: 'out_of_range' },
    ]);
    expect(validateCreate(input({ passphrase: '\ud800 twelve chars' }), limits)).toEqual([{ field: 'passphrase', code: 'malformed' }]);
  });

  it('warns about a passphrase below 16 characters or 4 words', () => {
    expect(passphraseIsWeak('twelve chars')).toBe(true);
    expect(passphraseIsWeak('sixteencharacter')).toBe(true);
    expect(passphraseIsWeak('one two three four')).toBe(false);
  });
});
