import {
  KDF_NONE,
  KDF_PBKDF2,
  KDF_SCRYPT,
  PowTimeoutError,
  deriveContent,
  deriveLink,
  derivePassphraseKey,
  encodeBase64url,
  formatLink,
  newHeader,
  newSecret,
  requestKey,
  seal,
  solvePow,
  type KdfId,
} from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { ApiError } from '../src/lib/api.ts';
import { CreateError, POW_BUDGET_MS, createMessage, passphraseIsWeak, validateCreate, type CreateDeps, type CreateInput } from '../src/lib/create.ts';
import { createReader, type ReadView, type SessionStore } from '../src/lib/read.ts';
import { fakeApi, type FakeApi, type FakeOptions } from './fake-api.ts';

const scrypt = (passphrase: string, saltPw: Uint8Array<ArrayBuffer>, onProgress: (fraction: number) => void) =>
  derivePassphraseKey(KDF_SCRYPT, passphrase, saltPw, { onProgress });

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
  return { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', kdfId: KDF_NONE, ...overrides };
}

function memorySession(): SessionStore & { map: Map<string, string>; log: string[] } {
  const map = new Map<string, string>();
  const log: string[] = [];
  return {
    map,
    log,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      log.push(`set ${key}`);
      map.set(key, value);
    },
    removeItem: (key) => {
      if (map.delete(key)) {
        log.push(`remove ${key}`);
      }
    },
  };
}

function reader(api: FakeApi, link: string, overrides: { session?: ReturnType<typeof memorySession>; visible?: boolean; prerendering?: boolean } = {}) {
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
    (view) => {
      views.push(view);
      api.calls.push(`view:${view.view}`);
    },
  );
  return { ...instance, views, last: () => views.at(-1), dropped: () => dropped, session };
}

function idTextOf(link: string): string {
  return /\/m\/([^#]+)/.exec(link)?.[1] ?? '';
}

async function storeRaw(api: FakeApi, meta: { text?: string; file?: { name: string; mime: string; size: number } }, file?: Uint8Array): Promise<string> {
  const { create } = await api.instance();
  const s = newSecret();
  const keys = await deriveLink(create.id, s);
  const content = await deriveContent(create.id, s, new Uint8Array(0));
  const object = await seal(content.k, newHeader(KDF_NONE), create.id, { opensMax: 1, ttlSeconds: 3600 }, { meta, file });
  const readKey = await requestKey(keys.r);
  const manageKey = await requestKey(newSecret());
  await api.create({ id: create.id, idToken: create.idToken, readKey: readKey.publicKey, manageKey: manageKey.publicKey, kdfId: KDF_NONE, opensMax: 1, ttlSeconds: 3600, object });
  return formatLink('https://nylzo.test', create.id, KDF_NONE, s);
}

describe('reading a message', () => {
  it('shows the text before it acknowledges the lease and then drops the fragment and the lease', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    expect(created.link).toMatch(/^https:\/\/nylzo\.test\/m\/[A-Za-z0-9_-]{22}#1\.0\.[A-Za-z0-9_-]{43}$/);
    const r = reader(api, created.link);
    await r.load();
    expect(r.last()).toMatchObject({ view: 'ready', resumable: false, deviceFailed: false });
    await r.reveal();
    expect(r.last()).toEqual({ view: 'revealed', text: 'ahoj', file: undefined, burnable: false });
    expect(api.calls.slice(api.calls.indexOf('claim'))).toEqual(['claim', 'view:busy', 'view:busy', 'view:revealed', 'ack']);
    expect(r.dropped()).toBe(1);
    const lease = `nylzo:lease:${idTextOf(created.link)}`;
    expect(r.session.log).toEqual([`set ${lease}`, `remove ${lease}`]);
  });

  it('rejects a wrong PBKDF2 passphrase without spending an open, then opens the file', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(70000).map((_, i) => i % 251);
    const created = await createMessage(
      input({ text: '', file: { name: 'report.pdf', mime: 'application/pdf', bytes }, passphrase: 'tři slova a ještě čtvrté', kdfId: KDF_PBKDF2 }),
      createDeps(api),
    );
    expect(created.link).toContain('#1.2.');
    const r = reader(api, created.link);
    await r.load();
    await r.reveal('špatná passphrase tady');
    expect(r.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong', attemptsLeft: 4, locked: false } });
    expect(r.views.find((v) => v.view === 'busy' && v.step === 'deriving')).toEqual({ view: 'busy', step: 'deriving' });
    expect(api.stored.get(idTextOf(created.link))?.opensRemaining).toBe(1);
    await r.reveal('tři slova a ještě čtvrté');
    const last = r.last();
    expect(last?.view === 'revealed' && last.file).toMatchObject({ name: 'report.pdf', mime: 'application/pdf', bytes });
    expect(api.calls.filter((c) => c.startsWith('claim'))).toEqual(['claim+verifier', 'claim+verifier']);
  });

  it('marks the scrypt derivation in the session and removes the mark afterwards', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_SCRYPT }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal('correct horse battery staple');
    expect(r.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    const id = idTextOf(created.link);
    expect(r.session.log).toEqual([`set nylzo:kdf:${id}`, `remove nylzo:kdf:${id}`, `set nylzo:lease:${id}`, `remove nylzo:lease:${id}`]);
    expect(r.views.filter((v) => v.view === 'busy' && v.step === 'deriving').length).toBeGreaterThan(2);
  });

  it('shows only the sanitised file name (SEC-1)', async () => {
    const api = fakeApi();
    const bytes = new TextEncoder().encode('MZ');
    const link = await storeRaw(api, { file: { name: 'invoice‮fdp.exe', mime: 'application/pdf', size: bytes.length } }, bytes);
    const r = reader(api, link);
    await r.load();
    await r.reveal();
    const last = r.last();
    expect(last?.view === 'revealed' && last.file?.name).toBe('invoicefdp.exe');
  });

  it('reports an opened tombstone as opened, not as tampered (SEC-2)', async () => {
    const options: FakeOptions = {};
    const api = fakeApi(options);
    const created = await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_PBKDF2 }), createDeps(api));
    const first = reader(api, created.link);
    await first.load();
    await first.reveal('correct horse battery staple');
    options.tombstone = true;
    const later = reader(api, created.link);
    await later.load();
    expect(later.last()).toEqual({ view: 'ended', state: 'opened', firstOpenedAt: '2026-10-23T10:00:00Z', endedAt: '2026-10-23T10:05:00Z' });
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

  it.each<[string, FakeOptions]>([
    ['another profile in X-Nylzo-Policy', { servedKdfId: KDF_PBKDF2 }],
    ['non-zero flags in X-Nylzo-Policy (TEST-S7)', { servedFlags: 1 }],
    ['opens_max in X-Nylzo-Policy that the object does not carry', { servedOpensMax: 2 }],
    ['ttl_s in X-Nylzo-Policy that the object does not carry', { servedTtlSeconds: 7200 }],
    ['metadata whose opens_max differs from X-Nylzo-Policy (SEC-9)', { metadataOpensMax: 2 }],
    ['metadata whose ttl_s differs from X-Nylzo-Policy (SEC-9)', { metadataTtlSeconds: 7200 }],
    ['a changed ciphertext', { tamperObject: true }],
  ])('treats %s as tampered and keeps the fragment', async (_, options) => {
    const api = fakeApi(options);
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal();
    expect(r.last()).toEqual({ view: 'tampered' });
    expect(r.dropped()).toBe(0);
    expect(api.calls).not.toContain('ack');
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
    const id = idTextOf(created.link);
    const stored = api.stored.get(id);
    await api.claim(stored?.request.id ?? new Uint8Array(16), { publicKey: new Uint8Array(stored?.request.readKey ?? []), privateKey: {} as CryptoKey }, {
      onLease: (lease) => session.setItem(`nylzo:lease:${id}`, lease),
    });
    const second = reader(api, created.link, { session });
    await second.load();
    expect(second.last()).toMatchObject({ view: 'ready', resumable: true });
    await second.reveal();
    expect(second.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    expect(api.calls.filter((c) => c === 'blob' || c === 'ack' || c.startsWith('claim'))).toEqual(['claim', 'blob', 'ack']);
    const third = reader(api, created.link);
    await third.load();
    expect(third.last()).toMatchObject({ view: 'ended', state: 'opened' });
  });

  it('lets the reader retry a mistyped passphrase on the lease path (SEC-7)', async () => {
    const api = fakeApi();
    const passphrase = 'correct horse battery staple';
    const created = await createMessage(input({ passphrase, kdfId: KDF_PBKDF2, opensMax: 2 }), createDeps(api));
    const session = memorySession();
    const first = reader(api, created.link, { session });
    await first.load();
    await first.reveal(passphrase);
    const id = idTextOf(created.link);
    const stored = api.stored.get(id);
    await api.claim(stored?.request.id ?? new Uint8Array(16), { publicKey: new Uint8Array(stored?.request.readKey ?? []), privateKey: {} as CryptoKey }, {
      verifierKey: { publicKey: new Uint8Array(stored?.request.verifierKey ?? []), privateKey: {} as CryptoKey },
      onLease: (lease) => session.setItem(`nylzo:lease:${id}`, lease),
    });
    const again = reader(api, created.link, { session });
    await again.load();
    await again.reveal('mistyped passphrase here');
    expect(again.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong_or_changed' } });
    await again.reveal(passphrase);
    expect(again.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
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
    session.setItem(`nylzo:kdf:${idTextOf(created.link)}`, '1');
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
    await createMessage(input({ passphrase: 'correct horse battery staple', kdfId: KDF_PBKDF2 }), deps);
    expect(api.calls.filter((c) => c === 'instance' || c === 'solve' || c === 'create')).toEqual(['instance', 'instance', 'solve', 'create']);
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

  it('warns unless the passphrase has 16 characters and 4 words (D37)', () => {
    expect(passphraseIsWeak('twelve chars')).toBe(true);
    expect(passphraseIsWeak('sixteencharacter')).toBe(true);
    expect(passphraseIsWeak('one two three four')).toBe(false);
  });
});

describe('deleting and reporting from the read page (D40, D41)', () => {
  it('deletes a message before showing it and then refuses to reveal it', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(await r.burn()).toEqual({ outcome: 'deleted' });
    expect(r.last()).toEqual({ view: 'ended', state: 'deleted', firstOpenedAt: undefined, endedAt: undefined });
    await r.reveal();
    expect(api.calls).not.toContain('claim');
    expect(await r.burn()).toEqual({ outcome: 'ignored' });
    expect(api.calls.filter((call) => call === 'burn')).toHaveLength(1);
  });

  it('deletes the remaining opens after showing the message and keeps the content on screen', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 3 }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal();
    expect(r.last()).toMatchObject({ view: 'revealed', text: 'ahoj', burnable: true });
    expect(await r.burn()).toEqual({ outcome: 'deleted' });
    expect(r.last()).toEqual({ view: 'revealed', text: 'ahoj', file: undefined, burnable: false });
    expect([...api.stored.values()][0]?.state).toBe('deleted');
    expect(await r.burn()).toEqual({ outcome: 'ignored' });
  });

  it('offers no deletion after the last open was shown', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.reveal();
    expect(r.last()).toMatchObject({ view: 'revealed', burnable: false });
    expect(await r.burn()).toEqual({ outcome: 'ignored' });
    expect(api.calls).not.toContain('burn');
  });

  it('counts the open of a resumed lease when it decides whether opens remain', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDeps(api));
    const session = memorySession();
    const first = reader(api, created.link, { session });
    await first.load();
    session.setItem(`nylzo:lease:${idTextOf(created.link)}`, 'lease-1');
    [...api.stored.values()][0]!.leases.add('lease-1');
    [...api.stored.values()][0]!.opensRemaining = 1;
    const resumed = reader(api, created.link, { session });
    await resumed.load();
    await resumed.reveal();
    expect(resumed.last()).toMatchObject({ view: 'revealed', burnable: true });
  });

  it('ignores a deletion while the message is being revealed or already being deleted', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    const revealing = r.reveal();
    expect(await r.burn()).toEqual({ outcome: 'ignored' });
    await revealing;
    expect(r.last()).toMatchObject({ view: 'revealed', burnable: true });
    const [first, second] = await Promise.all([r.burn(), r.burn()]);
    expect([first, second]).toEqual([{ outcome: 'deleted' }, { outcome: 'ignored' }]);
    expect(api.calls.filter((call) => call === 'burn')).toHaveLength(1);
  });

  it('does nothing on a hidden or prerendering page', async () => {
    for (const overrides of [{ visible: false }, { prerendering: true }]) {
      const api = fakeApi();
      const created = await createMessage(input(), createDeps(api));
      const r = reader(api, created.link, overrides);
      await r.load();
      expect(await r.burn()).toEqual({ outcome: 'ignored' });
      expect(await r.report('spam')).toEqual({ outcome: 'ignored' });
      expect(api.calls).not.toContain('burn');
      expect(api.calls).not.toContain('report');
    }
  });

  it('shows the end state when the message ended before the deletion', async () => {
    const api = fakeApi({ failBurn: [new ApiError(410, 'gone', undefined, undefined, 'cancelled')] });
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(await r.burn()).toEqual({ outcome: 'ended', state: 'cancelled' });
    expect(r.last()).toMatchObject({ view: 'ended', state: 'cancelled' });
  });

  it('keeps the page when the deletion is rate limited and allows another attempt', async () => {
    const api = fakeApi({ failBurn: [new ApiError(429, 'rate_limited', 30)] });
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(await r.burn()).toEqual({ outcome: 'retry_later', retryAfter: 30 });
    expect(r.last()).toMatchObject({ view: 'ready' });
    expect(await r.burn()).toEqual({ outcome: 'deleted' });
  });

  it('reports a message with the read key before and after it is shown', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(await r.report('phishing')).toEqual({ outcome: 'accepted' });
    await r.reveal();
    expect(await r.report('')).toEqual({ outcome: 'accepted' });
    expect(api.reports).toEqual([
      { id: idTextOf(created.link), signedByReader: true, reason: 'phishing' },
      { id: idTextOf(created.link), signedByReader: true, reason: '' },
    ]);
  });

  it('drops the read key once the page shows an end state', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    await r.burn();
    expect(await r.report('late')).toEqual({ outcome: 'ignored' });
    expect(api.reports).toEqual([]);
  });

  it('refuses a reason longer than 1000 bytes without a request', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDeps(api));
    const r = reader(api, created.link);
    await r.load();
    expect(await r.report('č'.repeat(501))).toEqual({ outcome: 'invalid', problems: ['reason_too_long'] });
    expect(api.calls).not.toContain('report');
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
