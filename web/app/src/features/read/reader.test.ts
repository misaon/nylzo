import {
  deriveContent,
  deriveLink,
  encodeBase64url,
  formatLink,
  KDF_NONE,
  KDF_SCRYPT,
  newHeader,
  newSecret,
  requestKey,
  seal,
} from '@nylzo/core';
import { scryptPassphraseKey } from '@nylzo/core/scrypt';
import { describe, expect, it, vi } from 'vitest';

import type { ResumeStore } from '../../shared/sent-records/resume-store.ts';

import { ApiError, type MessageState } from '../../shared/api/api-errors.ts';
import { abuseReport, fakeApi, type FakeApi, type FakeOptions } from '../../shared/api/fake-api.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import { newGate } from '../../shared/testing/gate.ts';
import { type CreateDependencies, type CreateInput, createMessage } from '../create/create-message.ts';
import { createReader, type ReaderDependencies, type ReadView, type SessionStore } from './reader.ts';

type Scrypt = ReaderDependencies['scrypt'];

const scrypt: Scrypt = (passphrase, passphraseSalt, onProgress) =>
  Promise.resolve(scryptPassphraseKey(passphrase, passphraseSalt, onProgress));

class ImpostorError extends Error {
  override readonly name = 'KdfWorkerError';
}

function refuseSigningKeyImport() {
  const importKey = crypto.subtle.importKey.bind(crypto.subtle);
  return vi
    .spyOn(crypto.subtle, 'importKey')
    .mockImplementation(((...parameters: Parameters<typeof importKey>) =>
      parameters[0] === 'pkcs8'
        ? Promise.reject(new DOMException('refused', 'DataError'))
        : importKey(...parameters)) as typeof crypto.subtle.importKey);
}

function workerScrypt(calls: string[] = []): Scrypt {
  return async (passphrase, passphraseSalt, onProgress) => {
    calls.push('scrypt');
    try {
      return await scrypt(passphrase, passphraseSalt, onProgress);
    } catch {
      throw new KdfWorkerError();
    }
  };
}

function createDependencies(api: FakeApi): CreateDependencies {
  return { api, origin: 'https://nylzo.test', scrypt, solvePow: () => Promise.resolve('') };
}

function input(overrides: Partial<CreateInput> = {}): CreateInput {
  return { text: 'ahoj', ttlSeconds: 3600, opensMax: 1, passphrase: '', ...overrides };
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

interface ReaderOverrides {
  readonly resume?: ResumeStore;
  readonly session?: ReturnType<typeof memorySession>;
  readonly visible?: boolean;
  readonly prerendering?: boolean;
  readonly scrypt?: Scrypt;
  readonly shown?: () => Promise<void>;
  readonly reloaded?: boolean;
}

const resumeStores = new WeakMap<SessionStore, ResumeStore>();

function resumeFor(session: SessionStore): ResumeStore {
  let resume = resumeStores.get(session);
  if (!resume) {
    const records = new Map<string, { id: string; fragment: string }>();
    resume = {
      put: (id, fragment) => {
        const token = encodeBase64url(crypto.getRandomValues(new Uint8Array(32)));
        records.set(token, { id, fragment });
        return Promise.resolve(token);
      },
      get: (id, token) => {
        const record = records.get(token);
        return Promise.resolve(record?.id === id ? record.fragment : undefined);
      },
      remove: (token) => {
        records.delete(token);
        return Promise.resolve();
      },
    };
    resumeStores.set(session, resume);
  }
  return resume;
}

function openReader(api: FakeApi, link: string, overrides: ReaderOverrides = {}) {
  const url = new URL(link);
  const views: ReadView[] = [];
  let dropped = 0;
  const session = overrides.session ?? memorySession();
  const instance = createReader(
    {
      api,
      location: { pathname: url.pathname, hash: overrides.reloaded ? '' : url.hash },
      session,
      resume: overrides.resume ?? resumeFor(session),
      page: { visible: () => overrides.visible ?? true, prerendering: () => overrides.prerendering ?? false },
      dropFragment: () => {
        dropped++;
        api.calls.push('drop-fragment');
      },
      ...(overrides.shown && { shown: overrides.shown }),
      scrypt: overrides.scrypt ?? scrypt,
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

async function storeRaw(
  api: FakeApi,
  meta: { text?: string; file?: { name: string; mime: string; size: number } },
  file?: Uint8Array,
): Promise<string> {
  const { create } = await api.instance();
  const secret = newSecret();
  const keys = await deriveLink(create.id, secret);
  const content = await deriveContent(create.id, secret, new Uint8Array(0));
  const object = await seal(
    content.contentKey,
    newHeader(KDF_NONE),
    create.id,
    { opensMax: 1, ttlSeconds: 3600 },
    { meta, file },
  );
  const readKey = await requestKey(keys.readSeed);
  const manageKey = await requestKey(newSecret());
  await api.create({
    id: create.id,
    idToken: create.idToken,
    readKey: readKey.publicKey,
    manageKey: manageKey.publicKey,
    kdfId: KDF_NONE,
    opensMax: 1,
    ttlSeconds: 3600,
    object,
  });
  return formatLink('https://nylzo.test', create.id, KDF_NONE, secret);
}

describe('reading a message', () => {
  it('keeps reading when recovery storage is unavailable, without storing a plaintext fallback', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const session = memorySession();
    const resume = { ...resumeFor(session), put: () => Promise.reject(new Error('storage refused')) };
    const reader = openReader(api, created.link, { session, resume });
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    expect(session.log).not.toContain(`set nylzo:link:${idTextOf(created.link)}`);
  });

  it('rejects and removes an old plaintext session fragment before making a request', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const session = memorySession();
    const id = idTextOf(created.link);
    session.setItem(`nylzo:link:${id}`, new URL(created.link).hash);
    session.setItem(`nylzo:lease:${id}`, 'old-lease');
    const before = api.calls.length;
    const reader = openReader(api, created.link, { session, reloaded: true });
    await reader.load();
    expect(reader.last()).toEqual({ view: 'removed' });
    expect(session.map.size).toBe(0);
    expect(api.calls.slice(before)).toEqual(['view:loading', 'view:removed']);
  });

  it('drops the fragment before its first request and acknowledges only once the page shows the message (D95)', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const display = newGate();
    const reader = openReader(api, created.link, { shown: () => display.opened });
    const before = api.calls.length;
    await reader.load();
    expect(reader.dropped()).toBe(1);
    expect(api.calls.slice(before).find((call) => !call.startsWith('view:'))).toBe('drop-fragment');
    const revealing = reader.reveal();
    await vi.waitFor(() => expect(reader.last()?.view).toBe('revealed'));
    expect(api.calls).not.toContain('ack');
    display.open();
    await revealing;
    expect(api.calls.at(-1)).toBe('ack');
    expect(reader.dropped()).toBe(1);
  });

  it('explains after a reload before the claim that the key left the address (D95)', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const before = api.calls.length;
    const reloaded = openReader(api, created.link, { session: reader.session, reloaded: true });
    await reloaded.load();
    expect(reloaded.last()).toEqual({ view: 'removed' });
    expect(api.calls.slice(before).filter((call) => !call.startsWith('view:'))).toEqual([]);
    const damaged = openReader(api, created.link.replace('#1.', '#0.'));
    await damaged.load();
    expect(damaged.last()).toEqual({ view: 'invalid' });
    expect(damaged.dropped()).toBe(1);
  });

  it('keeps the lease and the link in the session when the page is left before it shows the message', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const display = newGate();
    const reader = openReader(api, created.link, { shown: () => display.opened });
    await reader.load();
    const revealing = reader.reveal();
    await vi.waitFor(() => expect(reader.last()?.view).toBe('revealed'));
    reader.clear();
    display.open();
    await revealing;
    expect(api.calls).not.toContain('ack');
    expect(reader.last()).toEqual({ view: 'cleared' });
    expect(reader.session.map.get(`nylzo:link:${idTextOf(created.link)}`)).toMatch(/^[\w-]{43}$/);
    const again = openReader(api, created.link, { session: reader.session, reloaded: true });
    await again.load();
    expect(again.last()).toMatchObject({ view: 'ready', resumable: true });
  });

  it('shows the text before it acknowledges the lease and then forgets the lease and the link', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    expect(created.link).toMatch(/^https:\/\/nylzo\.test\/m\/[\w-]{22}#1\.0\.[\w-]{43}$/);
    const reader = openReader(api, created.link);
    await reader.load();
    expect(reader.last()).toMatchObject({ view: 'ready', resumable: false, deviceFailed: false });
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'revealed', text: 'ahoj', file: undefined, burnable: false });
    expect(api.calls.slice(api.calls.indexOf('claim'))).toEqual([
      'claim',
      'view:busy',
      'view:busy',
      'view:revealed',
      'ack',
    ]);
    expect(reader.dropped()).toBe(1);
    const id = idTextOf(created.link);
    expect(reader.session.log).toEqual([
      `set nylzo:dropped:${id}`,
      `set nylzo:lease:${id}`,
      `set nylzo:link:${id}`,
      `remove nylzo:lease:${id}`,
      `remove nylzo:link:${id}`,
    ]);
  });

  it('rejects a wrong passphrase without spending an open, then opens the file', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(70_000).map((_, index) => index % 251);
    const created = await createMessage(
      input({
        text: '',
        file: { name: 'report.pdf', mime: 'application/pdf', bytes },
        passphrase: 'tři slova a ještě čtvrté',
      }),
      createDependencies(api),
    );
    expect(created.link).toContain('#1.1.');
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal('špatná passphrase tady');
    expect(reader.last()).toMatchObject({
      view: 'ready',
      rejected: { reason: 'wrong', attemptsLeft: 4, locked: false },
    });
    expect(reader.views.find((readView) => readView.view === 'busy' && readView.step === 'deriving')).toEqual({
      view: 'busy',
      step: 'deriving',
      fraction: 0,
    });
    expect(api.stored.get(idTextOf(created.link))?.opensRemaining).toBe(1);
    await reader.reveal('tři slova a ještě čtvrté');
    const last = reader.last();
    expect(last?.view === 'revealed' && last.file).toMatchObject({
      name: 'report.pdf',
      mime: 'application/pdf',
      bytes,
    });
    expect(api.calls.filter((call) => call.startsWith('claim'))).toEqual(['claim+verifier', 'claim+verifier']);
  });

  it('marks the scrypt derivation in the session and removes the mark afterwards', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal('correct horse battery staple');
    expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    const id = idTextOf(created.link);
    expect(reader.session.log).toEqual([
      `set nylzo:dropped:${id}`,
      `set nylzo:kdf:${id}`,
      `remove nylzo:kdf:${id}`,
      `set nylzo:lease:${id}`,
      `set nylzo:link:${id}`,
      `remove nylzo:lease:${id}`,
      `remove nylzo:link:${id}`,
    ]);
    expect(
      reader.views.filter((readView) => readView.view === 'busy' && readView.step === 'deriving').length,
    ).toBeGreaterThan(2);
  });

  it('shows only the sanitised file name (SEC-1)', async () => {
    const api = fakeApi();
    const bytes = new TextEncoder().encode('MZ');
    const link = await storeRaw(
      api,
      { file: { name: 'invoice‮fdp.exe', mime: 'application/pdf', size: bytes.length } },
      bytes,
    );
    const reader = openReader(api, link);
    await reader.load();
    await reader.reveal();
    const last = reader.last();
    expect(last?.view === 'revealed' && last.file?.name).toBe('invoicefdp.exe');
  });

  it('reports an opened tombstone as opened, not as tampered (SEC-2)', async () => {
    const options: FakeOptions = {};
    const api = fakeApi(options);
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const first = openReader(api, created.link);
    await first.load();
    await first.reveal('correct horse battery staple');
    options.tombstone = true;
    const later = openReader(api, created.link);
    await later.load();
    expect(later.last()).toEqual({
      view: 'ended',
      state: 'opened',
      firstOpenedAt: '2026-10-23T10:00:00Z',
      endedAt: '2026-10-23T10:05:00Z',
    });
  });

  it('stops before any verifier when the server reports another profile (AK24)', async () => {
    const api = fakeApi({ metadataKdfId: KDF_NONE });
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    expect(reader.last()).toEqual({ view: 'tampered' });
    await reader.reveal('correct horse battery staple');
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
  });

  it.each<[string, FakeOptions]>([
    ['another profile in X-Nylzo-Policy', { servedKdfId: KDF_SCRYPT }],
    ['non-zero flags in X-Nylzo-Policy (TEST-S7)', { servedFlags: 1 }],
    ['opens_max in X-Nylzo-Policy that the object does not carry', { servedOpensMax: 2 }],
    ['ttl_s in X-Nylzo-Policy that the object does not carry', { servedTtlSeconds: 7200 }],
    ['metadata whose opens_max differs from X-Nylzo-Policy (SEC-9)', { metadataOpensMax: 2 }],
    ['metadata whose ttl_s differs from X-Nylzo-Policy (SEC-9)', { metadataTtlSeconds: 7200 }],
    ['a changed ciphertext', { tamperObject: true }],
  ])('treats %s as tampered', async (_, options) => {
    const api = fakeApi(options);
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'tampered' });
    expect(api.calls).not.toContain('ack');
  });

  it('never claims from a hidden or prerendered page', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    for (const overrides of [{ visible: false }, { prerendering: true }]) {
      const reader = openReader(api, created.link, overrides);
      await reader.load();
      await reader.reveal();
      expect(reader.last()?.view).toBe('ready');
    }
    expect(api.calls.filter((call) => call === 'claim')).toEqual([]);
  });

  it('resumes from the stored lease after a reload instead of spending another open', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const session = memorySession();
    const id = idTextOf(created.link);
    const stored = api.stored.get(id);
    await api.claim(
      stored?.request.id ?? new Uint8Array(16),
      { publicKey: new Uint8Array(stored?.request.readKey ?? []), privateKey: {} as CryptoKey },
      {
        onLease: (lease) => session.setItem(`nylzo:lease:${id}`, lease),
      },
    );
    const second = openReader(api, created.link, { session });
    await second.load();
    expect(second.last()).toMatchObject({ view: 'ready', resumable: true });
    await second.reveal();
    expect(second.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    expect(api.calls.filter((call) => call === 'blob' || call === 'ack' || call.startsWith('claim'))).toEqual([
      'claim',
      'blob',
      'ack',
    ]);
    const third = openReader(api, created.link);
    await third.load();
    expect(third.last()).toMatchObject({ view: 'ended', state: 'opened' });
  });

  it('lets the reader retry a mistyped passphrase on the lease path (SEC-7)', async () => {
    const api = fakeApi();
    const passphrase = 'correct horse battery staple';
    const created = await createMessage(input({ passphrase, opensMax: 2 }), createDependencies(api));
    const session = memorySession();
    const first = openReader(api, created.link, { session });
    await first.load();
    await first.reveal(passphrase);
    const id = idTextOf(created.link);
    const stored = api.stored.get(id);
    await api.claim(
      stored?.request.id ?? new Uint8Array(16),
      { publicKey: new Uint8Array(stored?.request.readKey ?? []), privateKey: {} as CryptoKey },
      {
        verifierKey: { publicKey: new Uint8Array(stored?.request.verifierKey ?? []), privateKey: {} as CryptoKey },
        onLease: (lease) => session.setItem(`nylzo:lease:${id}`, lease),
      },
    );
    const again = openReader(api, created.link, { session });
    await again.load();
    await again.reveal('mistyped passphrase here');
    expect(again.last()).toMatchObject({ view: 'ready', rejected: { reason: 'wrong_or_changed' } });
    await again.reveal(passphrase);
    expect(again.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
  });

  it('tells the reader when wrong attempts locked the message for its lifetime', async () => {
    const api = fakeApi({ lockAfter: 1, expiresInSeconds: 600 });
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal('nesprávná passphrase');
    expect(reader.last()).toMatchObject({
      view: 'ready',
      rejected: { reason: 'wrong', attemptsLeft: 0, retryAfter: 3600, locked: true },
    });
  });

  it('warns after a key derivation that the system ended', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const session = memorySession();
    session.setItem(`nylzo:kdf:${idTextOf(created.link)}`, '1');
    const reader = openReader(api, created.link, { session });
    await reader.load();
    expect(reader.last()).toMatchObject({ view: 'ready', deviceFailed: true });
    expect(Object.fromEntries(session.map)).toEqual({ [`nylzo:dropped:${idTextOf(created.link)}`]: '1' });
  });

  it('tells a malformed link, a link from a newer version, one with the retired profile and an unknown one apart', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const before = api.calls.length;
    const broken = openReader(api, created.link.replace('#1.', '#0.'));
    await broken.load();
    expect(broken.last()).toEqual({ view: 'invalid' });
    const brokenPath = openReader(api, created.link.replace('/m/', '/m/x'));
    await brokenPath.load();
    expect(brokenPath.last()).toEqual({ view: 'invalid' });
    for (const newerLink of [
      created.link.replace('#1.', '#2.'),
      created.link.replace('/m/', '/m/x').replace(/#.*$/, '#10.a.b.c'),
    ]) {
      const newer = openReader(api, newerLink);
      await newer.load();
      expect(newer.last()).toEqual({ view: 'newer' });
    }
    const retired = openReader(api, created.link.replace('#1.0.', '#1.2.'));
    await retired.load();
    expect(retired.last()).toEqual({ view: 'retired' });
    const retiredBadPath = openReader(api, created.link.replace('/m/', '/m/x').replace('#1.0.', '#1.2.'));
    await retiredBadPath.load();
    expect(retiredBadPath.last()).toEqual({ view: 'invalid' });
    expect(api.calls.slice(before).filter((call) => !call.startsWith('view:') && call !== 'drop-fragment')).toEqual([]);
    const other = encodeBase64url(new Uint8Array(32).fill(5));
    const unknown = openReader(
      api,
      created.link.replace(/#1\.0\..*$/, () => `#1.0.${other}`),
    );
    await unknown.load();
    expect(unknown.last()).toEqual({ view: 'unknown' });
  });
});

function leaseKeyOf(link: string): string {
  return `nylzo:lease:${idTextOf(link)}`;
}

function storedOf(api: FakeApi, link: string) {
  const stored = api.stored.get(idTextOf(link));
  if (!stored) {
    throw new Error('the fake server does not know the message');
  }
  return stored;
}

describe('reading through the lease and claim edge cases', () => {
  it('asks before spending another open when the stored lease ran out', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 3 }), createDependencies(api));
    const session = memorySession();
    session.setItem(leaseKeyOf(created.link), 'stale');
    const reader = openReader(api, created.link, { session });
    await reader.load();
    expect(reader.last()).toMatchObject({ view: 'ready', resumable: true });
    api.calls.length = 0;
    await reader.reveal();
    expect(api.calls.filter((call) => call === 'blob' || call.startsWith('claim'))).toEqual(['blob']);
    expect(reader.last()).toMatchObject({ view: 'ready', resumable: false, leaseEnded: true });
    expect(session.map.has(leaseKeyOf(created.link))).toBe(false);
    expect(storedOf(api, created.link).opensRemaining).toBe(3);
    await reader.reveal();
    expect(api.calls.filter((call) => call === 'blob' || call.startsWith('claim'))).toEqual(['blob', 'claim']);
    expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj', burnable: true });
    expect(storedOf(api, created.link).opensRemaining).toBe(2);
  });

  it('keeps the lease when the download limit of the lease is reached', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const session = memorySession();
    session.setItem(leaseKeyOf(created.link), 'lease-1');
    api.blob = () => {
      api.calls.push('blob');
      return Promise.reject(new ApiError(429, 'rate_limited', { retryAfter: 90 }));
    };
    const reader = openReader(api, created.link, { session });
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'retry_later', retryAfter: 90 });
    expect(session.map.get(leaseKeyOf(created.link))).toBe('lease-1');
    expect(api.calls.filter((call) => call === 'blob' || call.startsWith('claim'))).toEqual(['blob']);
  });

  it('keeps the lease when the claim stream breaks, so a reload resumes without another open', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const session = memorySession();
    const claim = api.claim.bind(api);
    api.claim = async (id, readKey, options = {}) => {
      await claim(id, readKey, options);
      throw new TypeError('network error');
    };
    const first = openReader(api, created.link, { session });
    await first.load();
    await first.reveal();
    expect(first.last()).toEqual({ view: 'failed' });
    expect(session.map.get(leaseKeyOf(created.link))).toBe('lease-1');
    api.claim = claim;
    const again = openReader(api, created.link, { session });
    await again.load();
    expect(again.last()).toMatchObject({ view: 'ready', resumable: true });
    await again.reveal();
    expect(again.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
    expect(api.calls.filter((call) => call === 'blob' || call.startsWith('claim'))).toEqual(['claim', 'blob']);
  });

  it.each<MessageState>(['cancelled', 'deleted', 'expired', 'opened'])(
    'shows the end state %s when the message ended after the page loaded',
    async (state) => {
      const api = fakeApi();
      const created = await createMessage(input(), createDependencies(api));
      const reader = openReader(api, created.link);
      await reader.load();
      reader.session.setItem(leaseKeyOf(created.link), 'stale');
      storedOf(api, created.link).state = state;
      await reader.reveal();
      expect(reader.last()).toMatchObject({ view: 'ended', state });
      expect(reader.session.map.has(leaseKeyOf(created.link))).toBe(false);
      expect(await reader.report(abuseReport('late'))).toEqual({ outcome: 'ignored' });
    },
  );

  it.each([
    [429, 'rate_limited', 7],
    [503, 'unavailable', 5],
  ] as const)('waits after a %i claim without spending an open', async (status, kind, wait) => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    api.claim = () => {
      api.calls.push('claim');
      return Promise.reject(new ApiError(status, kind, { retryAfter: wait }));
    };
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'retry_later', retryAfter: wait });
    expect(reader.session.map.has(leaseKeyOf(created.link))).toBe(false);
    expect(storedOf(api, created.link).opensRemaining).toBe(1);
  });

  it('reports a link that became unknown at the claim', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    api.claim = () => Promise.reject(new ApiError(404, 'not_found'));
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'unknown' });
  });

  it('sends one claim when the reader submits twice before the first reveal ends', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const claim = api.claim.bind(api);
    api.claim = async (...parameters: Parameters<typeof claim>) => {
      await new Promise((resolve) => setTimeout(resolve, 50));
      return claim(...parameters);
    };
    await Promise.all([reader.reveal(), reader.reveal()]);
    expect(api.calls.filter((call) => call.startsWith('claim'))).toEqual(['claim']);
    expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj' });
  });
});

describe('passphrase handling on the read page', () => {
  it('refuses a malformed passphrase before any derivation or claim', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const derivations: string[] = [];
    const reader = openReader(api, created.link, { scrypt: workerScrypt(derivations) });
    await reader.load();
    await reader.reveal('caf\u{D800} correct horse');
    expect(reader.last()).toMatchObject({ view: 'ready', deviceFailed: false, rejected: { reason: 'malformed' } });
    expect(reader.views.some((readView) => readView.view === 'busy')).toBe(false);
    expect(derivations).toEqual([]);
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
    expect(Object.fromEntries(reader.session.map)).toEqual({ [`nylzo:dropped:${idTextOf(created.link)}`]: '1' });
  });

  it('suggests another device when the key derivation worker fails', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const reader = openReader(api, created.link, {
      scrypt: () => Promise.reject(new KdfWorkerError()),
    });
    await reader.load();
    await reader.reveal('correct horse battery staple');
    expect(reader.last()).toMatchObject({ view: 'ready', deviceFailed: true, rejected: undefined });
    const other = openReader(api, created.link, {
      scrypt: () => Promise.reject(new ImpostorError('not the worker')),
    });
    await other.load();
    await other.reveal('correct horse battery staple');
    expect(other.last()).toEqual({ view: 'failed' });
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
  });
});

describe('leaving the page', () => {
  it('wipes the shown file and drops the read key', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(5000).fill(7);
    const created = await createMessage(
      input({ text: '', file: { name: 'a.pdf', mime: 'application/pdf', bytes }, opensMax: 3 }),
      createDependencies(api),
    );
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    const shown = reader.last();
    const file = shown?.view === 'revealed' ? shown.file : undefined;
    expect(file?.bytes).toEqual(bytes);
    reader.clear();
    expect(reader.last()).toEqual({ view: 'cleared' });
    expect(file?.bytes.every((byte) => byte === 0)).toBe(true);
    expect(await reader.burn()).toEqual({ outcome: 'ignored' });
    expect(await reader.report(abuseReport('after leaving'))).toEqual({ outcome: 'ignored' });
    expect(api.calls).not.toContain('burn');
    expect(api.calls).not.toContain('report');
  });

  it('sends no claim when the page was left while the request was being signed', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const claim = api.claim.bind(api);
    api.claim = (...parameters: Parameters<typeof claim>) => {
      reader.clear();
      return claim(...parameters);
    };
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'cleared' });
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
  });

  it('reveals nothing once the page was left before the reveal', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    reader.clear();
    expect(reader.last()).toEqual({ view: 'cleared' });
    await reader.reveal();
    expect(reader.last()).toEqual({ view: 'cleared' });
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
  });

  it('keeps the message off the page when a deletion answers after the page was left', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(5000).fill(7);
    const created = await createMessage(
      input({ file: { name: 'a.pdf', mime: 'application/pdf', bytes }, opensMax: 2 }),
      createDependencies(api),
    );
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    const shown = reader.last();
    expect(shown).toMatchObject({ view: 'revealed', burnable: true });
    const gate = newGate();
    const burn = api.burn.bind(api);
    api.burn = async (...parameters: Parameters<typeof burn>) => {
      await gate.opened;
      return burn(...parameters);
    };
    const burning = reader.burn();
    reader.clear();
    const after = reader.views.length;
    gate.open();
    expect(await burning).toEqual({ outcome: 'deleted' });
    expect(reader.views.slice(after - 1)).toEqual([{ view: 'cleared' }]);
    expect(shown?.view === 'revealed' && shown.file?.bytes.every((byte) => byte === 0)).toBe(true);
  });

  it('keeps a reveal that finishes after the page was left off the page, and a reload resumes it without another open', async () => {
    const api = fakeApi();
    const bytes = new Uint8Array(5000).fill(7);
    const created = await createMessage(
      input({ text: 'later', file: { name: 'a.pdf', mime: 'application/pdf', bytes } }),
      createDependencies(api),
    );
    const session = memorySession();
    const reader = openReader(api, created.link, { session });
    await reader.load();
    const gate = newGate();
    const asked = newGate();
    const claim = api.claim.bind(api);
    let claimed: Uint8Array | undefined;
    api.claim = async (id, readKey, options = {}) => {
      asked.open();
      await gate.opened;
      const envelope = await claim(id, readKey, { ...options, proceed: () => true });
      claimed = envelope.object;
      return envelope;
    };
    const revealing = reader.reveal();
    await asked.opened;
    reader.clear();
    const after = reader.views.length;
    gate.open();
    await revealing;
    expect(reader.views.slice(after - 1).every((view) => view.view === 'cleared')).toBe(true);
    expect(claimed).toBeDefined();
    expect(api.calls).not.toContain('ack');
    expect(session.map.get(leaseKeyOf(created.link))).toBe('lease-1');
    api.claim = claim;
    const reloaded = openReader(api, created.link, { session, reloaded: true });
    await reloaded.load();
    expect(reloaded.last()).toMatchObject({ view: 'ready', resumable: true });
    await reloaded.reveal();
    expect(reloaded.last()).toMatchObject({ view: 'revealed', text: 'later' });
    expect(api.calls.filter((call) => call === 'blob' || call.startsWith('claim'))).toEqual(['claim', 'blob']);
  });

  it('sends no claim when the page was left during the key derivation', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ passphrase: 'correct horse battery staple' }), createDependencies(api));
    const gate = newGate();
    const asked = newGate();
    const reader = openReader(api, created.link, {
      scrypt: async (...parameters) => {
        asked.open();
        await gate.opened;
        return scrypt(...parameters);
      },
    });
    await reader.load();
    const revealing = reader.reveal('correct horse battery staple');
    await asked.opened;
    reader.clear();
    gate.open();
    await revealing;
    expect(reader.last()).toEqual({ view: 'cleared' });
    expect(api.calls.some((call) => call.startsWith('claim'))).toBe(false);
    expect(storedOf(api, created.link).failures).toBe(0);
  });
});

describe('deleting and reporting from the read page (D40, D41)', () => {
  it('deletes a message before showing it and then refuses to reveal it', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    expect(await reader.burn()).toEqual({ outcome: 'deleted' });
    expect(reader.last()).toEqual({ view: 'ended', state: 'deleted', firstOpenedAt: undefined, endedAt: undefined });
    await reader.reveal();
    expect(api.calls).not.toContain('claim');
    expect(await reader.burn()).toEqual({ outcome: 'ignored' });
    expect(api.calls.filter((call) => call === 'burn')).toHaveLength(1);
  });

  it('deletes the remaining opens after showing the message and keeps the content on screen', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 3 }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj', burnable: true });
    expect(await reader.burn()).toEqual({ outcome: 'deleted' });
    expect(reader.last()).toEqual({ view: 'revealed', text: 'ahoj', file: undefined, burnable: false });
    expect(storedOf(api, created.link).state).toBe('deleted');
    expect(await reader.burn()).toEqual({ outcome: 'ignored' });
  });

  it('offers no deletion after the last open was shown', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toMatchObject({ view: 'revealed', burnable: false });
    expect(await reader.burn()).toEqual({ outcome: 'ignored' });
    expect(api.calls).not.toContain('burn');
  });

  it('counts the open of a resumed lease when it decides whether opens remain', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const session = memorySession();
    const first = openReader(api, created.link, { session });
    await first.load();
    session.setItem(`nylzo:lease:${idTextOf(created.link)}`, 'lease-1');
    storedOf(api, created.link).leases.add('lease-1');
    storedOf(api, created.link).opensRemaining = 1;
    const resumed = openReader(api, created.link, { session });
    await resumed.load();
    await resumed.reveal();
    expect(resumed.last()).toMatchObject({ view: 'revealed', burnable: true });
  });

  it('deletes the rest right after the reveal, after the ack, instead of ignoring the click', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const gate = newGate();
    const ack = api.ack.bind(api);
    api.ack = async (...parameters: Parameters<typeof ack>) => {
      await gate.opened;
      return ack(...parameters);
    };
    await reader.reveal();
    expect(reader.last()).toMatchObject({ view: 'revealed', burnable: true });
    const burning = reader.burn();
    await Promise.resolve();
    expect(api.calls).not.toContain('burn');
    gate.open();
    expect(await burning).toEqual({ outcome: 'deleted' });
    const order = api.calls.filter((call) => call === 'ack' || call === 'burn');
    expect(order).toEqual(['ack', 'burn']);
    expect(reader.last()).toMatchObject({ view: 'revealed', burnable: false });
  });

  it('ignores a deletion while the message is being revealed or already being deleted', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const revealing = reader.reveal();
    expect(await reader.burn()).toEqual({ outcome: 'ignored' });
    await revealing;
    expect(reader.last()).toMatchObject({ view: 'revealed', burnable: true });
    const [first, second] = await Promise.all([reader.burn(), reader.burn()]);
    expect([first, second]).toEqual([{ outcome: 'deleted' }, { outcome: 'ignored' }]);
    expect(api.calls.filter((call) => call === 'burn')).toHaveLength(1);
  });

  it('does nothing on a hidden or prerendering page', async () => {
    for (const overrides of [{ visible: false }, { prerendering: true }]) {
      const api = fakeApi();
      const created = await createMessage(input(), createDependencies(api));
      const reader = openReader(api, created.link, overrides);
      await reader.load();
      expect(await reader.burn()).toEqual({ outcome: 'ignored' });
      expect(await reader.report(abuseReport('spam'))).toEqual({ outcome: 'ignored' });
      expect(api.calls).not.toContain('burn');
      expect(api.calls).not.toContain('report');
    }
  });

  it('shows the end state when the message ended before the deletion', async () => {
    const api = fakeApi({ failBurn: [new ApiError(410, 'gone', { state: 'cancelled' })] });
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    expect(await reader.burn()).toEqual({ outcome: 'ended', state: 'cancelled' });
    expect(reader.last()).toMatchObject({ view: 'ended', state: 'cancelled' });
  });

  it('keeps the page when the deletion is rate limited and allows another attempt', async () => {
    const api = fakeApi({ failBurn: [new ApiError(429, 'rate_limited', { retryAfter: 30 })] });
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    expect(await reader.burn()).toEqual({ outcome: 'retry_later', retryAfter: 30 });
    expect(reader.last()).toMatchObject({ view: 'ready' });
    expect(await reader.burn()).toEqual({ outcome: 'deleted' });
  });

  it('reports a message with the read key before and after it is shown', async () => {
    const api = fakeApi();
    const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    expect(await reader.report(abuseReport('phishing'))).toEqual({ outcome: 'accepted' });
    await reader.reveal();
    expect(await reader.report(abuseReport(''))).toEqual({ outcome: 'accepted' });
    expect(api.reports).toEqual([
      { id: idTextOf(created.link), signedByReader: true, ...abuseReport('phishing') },
      { id: idTextOf(created.link), signedByReader: true, ...abuseReport('') },
    ]);
  });

  it('sends one report at a time', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    const [first, second] = await Promise.all([reader.report(abuseReport('one')), reader.report(abuseReport('two'))]);
    expect([first, second]).toEqual([{ outcome: 'accepted' }, { outcome: 'ignored' }]);
    expect(api.reports.map((report) => report.reason)).toEqual(['one']);
  });

  it('sends no report once the last open was shown, because the server keeps reports only for live messages (D46)', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.reveal();
    expect(reader.last()).toMatchObject({ view: 'revealed', burnable: false });
    expect(await reader.report(abuseReport('after the last open'))).toEqual({ outcome: 'ignored' });
    const rest = await createMessage(input({ opensMax: 2 }), createDependencies(api));
    const other = openReader(api, rest.link);
    await other.load();
    await other.reveal();
    expect(await other.burn()).toEqual({ outcome: 'deleted' });
    expect(await other.report(abuseReport('after deleting the rest'))).toEqual({ outcome: 'ignored' });
    expect(api.calls).not.toContain('report');
  });

  it('drops the read key once the page shows an end state', async () => {
    const api = fakeApi();
    const created = await createMessage(input(), createDependencies(api));
    const reader = openReader(api, created.link);
    await reader.load();
    await reader.burn();
    expect(await reader.report(abuseReport('late'))).toEqual({ outcome: 'ignored' });
    expect(api.reports).toEqual([]);
  });
});

describe('native signing after a PKCS#8 import failure (WebKit on Linux, D66)', () => {
  it('reads and deletes with a non-extractable native key after PKCS#8 refusal', async () => {
    const refuse = refuseSigningKeyImport();
    try {
      const api = fakeApi();
      const created = await createMessage(input({ opensMax: 2 }), createDependencies(api));
      const reader = openReader(api, created.link);
      await reader.load();
      await reader.reveal();
      expect(reader.last()).toMatchObject({ view: 'revealed', text: 'ahoj', burnable: true });
      const gate = newGate();
      let usedKey: { privateKey: CryptoKey } | undefined;
      let intactWhenSent = false;
      const burn = api.burn.bind(api);
      api.burn = async (...parameters: Parameters<typeof burn>) => {
        await gate.opened;
        usedKey = parameters[1];
        intactWhenSent = !usedKey.privateKey.extractable;
        return burn(...parameters);
      };
      const burning = reader.burn();
      reader.clear();
      gate.open();
      expect(await burning).toEqual({ outcome: 'deleted' });
      expect(usedKey?.privateKey).toBeInstanceOf(CryptoKey);
      expect(intactWhenSent).toBe(true);
      expect(usedKey?.privateKey.extractable).toBe(false);
    } finally {
      refuse.mockRestore();
    }
  });

  it('uses a native key to acknowledge the last open after PKCS#8 refusal', async () => {
    const refuse = refuseSigningKeyImport();
    try {
      const api = fakeApi();
      const created = await createMessage(input(), createDependencies(api));
      const reader = openReader(api, created.link);
      await reader.load();
      let usedKey: { privateKey: CryptoKey } | undefined;
      const ack = api.ack.bind(api);
      api.ack = (...parameters: Parameters<typeof ack>) => {
        usedKey = parameters[1];
        return ack(...parameters);
      };
      await reader.reveal();
      expect(reader.last()).toMatchObject({ view: 'revealed', burnable: false });
      await vi.waitFor(() => expect(usedKey?.privateKey.extractable).toBe(false));
    } finally {
      refuse.mockRestore();
    }
  });

  it('keeps a native key usable for an ack that is still on its way when the page is left', async () => {
    const refuse = refuseSigningKeyImport();
    try {
      const api = fakeApi();
      const created = await createMessage(input(), createDependencies(api));
      const reader = openReader(api, created.link);
      await reader.load();
      const gate = newGate();
      let usedKey: { privateKey: CryptoKey } | undefined;
      let intactWhenSent = false;
      const ack = api.ack.bind(api);
      api.ack = async (...parameters: Parameters<typeof ack>) => {
        await gate.opened;
        usedKey = parameters[1];
        intactWhenSent = !usedKey.privateKey.extractable;
        return ack(...parameters);
      };
      await reader.reveal();
      reader.clear();
      gate.open();
      await vi.waitFor(() => expect(usedKey?.privateKey.extractable).toBe(false));
      expect(intactWhenSent).toBe(true);
    } finally {
      refuse.mockRestore();
    }
  });
});
