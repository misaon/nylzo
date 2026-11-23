import { generatePassphrase, KDF_NONE, KDF_SCRYPT, loadPassphraseWords, parseLink, requestKey } from '@nylzo/core';
import { DetachedWindowAPI } from 'happy-dom';
import { flushSync } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Mock } from 'vitest';

import { createApi } from '../../../shared/api/api.ts';
import { fakeApi } from '../../../shared/api/fake-api.ts';
import type { InstanceLimits } from '../../../shared/api/responses.ts';
import { KdfWorkerError } from '../../../shared/kdf/kdf-protocol.ts';
import { startWorker } from '../../../shared/kdf/worker-source.ts';
import { formatBytes, formatDuration } from '../../../shared/language/format.ts';
import { Translator } from '../../../shared/language/translator.svelte.ts';
import { memoryStore } from '../../../shared/sent-records/memory-sent-store.ts';
import { browserSentStore, rememberSent } from '../../../shared/sent-records/sent-store.ts';
import { CreateError, createMessage } from '../create-message.ts';
import type { Created, CreateFailure, CreateProblem } from '../create-message.ts';
import { Composer } from './composer.svelte.ts';

vi.mock(import('@nylzo/core'), (original) =>
  original().then((actual) => ({
    ...actual,
    generatePassphrase: vi.fn<typeof actual.generatePassphrase>(actual.generatePassphrase),
    loadPassphraseWords: vi.fn<typeof actual.loadPassphraseWords>(actual.loadPassphraseWords),
  })),
);
vi.mock('../../../shared/api/api.ts', () => ({
  createApi: vi.fn<typeof import('../../../shared/api/api.ts').createApi>(),
}));
vi.mock('../../../shared/kdf/worker-source.ts', () => ({
  startWorker: vi.fn<typeof import('../../../shared/kdf/worker-source.ts').startWorker>(),
}));
vi.mock('../../../shared/sent-records/sent-store.ts', () => ({
  browserSentStore: vi.fn<(...arguments_: unknown[]) => void>(),
  rememberSent: vi.fn<(...arguments_: unknown[]) => void>(),
}));
// Keep validation real; only replace the encryption/upload boundary.
vi.mock(import('../create-message.ts'), (original) =>
  original().then((actual) => ({ ...actual, createMessage: vi.fn<typeof actual.createMessage>() })),
);

const limits: InstanceLimits = {
  maxFileBytes: 16,
  maxTextBytes: 102_400,
  maxObjectBytes: 5_373_952,
  maxTtlSeconds: 604_800,
  defaultTtlSeconds: 86_400,
  maxOpens: 10,
};

function reducedMotion(matches: boolean): void {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  environment.happyDOM.settings.device.prefersReducedMotion = matches ? 'reduce' : 'no-preference';
  dispatchEvent(new Event('resize'));
  flushSync();
}

describe('message composer interactions', () => {
  let created: Created;
  let composers: Composer[];

  const session = memoryStore();
  let browser: ReturnType<typeof memoryStore> & { readonly close: () => void };

  function composer(): Composer {
    const form = new Composer(new Translator('en'), session);
    form.limits = limits;
    composers.push(form);
    return form;
  }

  const reload = vi.fn<(...arguments_: unknown[]) => void>();

  beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    composers = [];
    vi.mocked(createApi).mockReturnValue(fakeApi());
    created = {
      id: 'AAAAAAAAAAAAAAAAAAAAAA',
      link: 'https://nylzo.test/m/AAAAAAAAAAAAAAAAAAAAAA#secret',
      expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
      manageKey: await requestKey(new Uint8Array(32).fill(7)),
    };
    vi.mocked(createMessage).mockResolvedValue(created);
    browser = Object.assign(memoryStore(), { close: vi.fn<() => void>() });
    vi.mocked(browserSentStore).mockReturnValue(browser);
    vi.mocked(rememberSent).mockResolvedValue(true);
    vi.stubGlobal('location', { origin: 'https://nylzo.test', reload });
  });

  afterEach(() => {
    for (const form of composers) {
      form.leave();
    }
    vi.useRealTimers();
    vi.unstubAllGlobals();
    document.head.replaceChildren();
  });

  it('loads instance limits, clamps opens and allows retry after a loading failure', async () => {
    const api = fakeApi();
    const instance = await api.instance();
    const load = vi.spyOn(api, 'instance').mockRejectedValueOnce(new Error('offline'));
    load.mockResolvedValue({ ...instance, limits: { ...limits, maxOpens: 2, defaultTtlSeconds: 7200 } });
    vi.mocked(createApi).mockReturnValue(api);
    const form = composer();
    form.opensMax = 8;
    await form.loadLimits();
    expect(form.loadFailed).toBe(true);
    expect(form.notice).toBe(new Translator('en').translate('failure.instance'));
    await form.loadLimits();
    expect(form.loadFailed).toBe(false);
    expect(form.notice).toBe('');
    expect(form.opensMax).toBe(2);
    expect(form.ttlSeconds).toBe(7200);
    expect(form.ttlOptions).toContain(7200);
    expect(form.ttlLabel(7200)).toBe(formatDuration(7200, 'en'));
    expect(form.fileLimit).toBe(16);
  });

  it('falls back to the format limits and the chosen lifetime before the instance answers', () => {
    const form = new Composer(new Translator('en'), session);
    composers.push(form);
    form.ttlSeconds = 3600;
    expect([form.textLimit, form.maxOpens, form.ttlOptions]).toEqual([102_400, 1, [3600]]);
    expect(form.fileLimit).toBeGreaterThan(limits.maxFileBytes);
  });

  it('tracks only in the tab, never in browser storage, when served from the verified local client', () => {
    const instance = document.createElement('meta');
    instance.name = 'nylzo-instance';
    instance.content = 'https://instance.test';
    document.head.append(instance);
    const form = composer();
    const stop = form.start(document.createElement('section'));
    expect(form.canKeep).toBe(false);
    form.text = 'secret';
    if (typeof stop === 'function') {
      stop();
    }
    expect([form.text, form.left]).toEqual(['', true]);
  });

  it('does not submit before limits load, and focuses empty content instead of sending it', async () => {
    const form = composer();
    form.limits = undefined;
    await submit(form);
    expect(createMessage).not.toHaveBeenCalled();
    form.limits = limits;
    const focus = focusTarget(form, 'text');
    await submit(form);
    expect(form.problems).toContainEqual({ field: 'content', code: 'missing' });
    expect(form.notice).not.toBe('');
    expect(focus).toHaveBeenCalledOnce();
    expect(createMessage).not.toHaveBeenCalled();
  });

  it('requires a passphrase when protection is enabled and clears it when disabled or forgotten', async () => {
    const form = composer();
    form.text = 'synthetic message';
    const focused = focusTarget(form, 'passphrase');
    await form.toggleProtect();
    expect(form.protect).toBe(true);
    expect(form.passphrase).not.toBe('');
    expect(focused).toHaveBeenCalledOnce();
    form.passphrase = '';
    await submit(form);
    expect(form.menu).toBe('security');
    expect(form.problems).toContainEqual({ field: 'passphrase', code: 'too_short' });
    expect(form.passphraseMessage).not.toBe('');
    expect(form.notice).toBe('');
    form.menu = undefined;
    expect(form.notice).toBe(form.passphraseMessage);
    expect(createMessage).not.toHaveBeenCalled();
    await form.generatePassphrase();
    expect(form.problems).toEqual([]);
    form.forgetPassphrase();
    expect(form.passphrase).toBe('');
    form.passphrase = 'password';
    expect(form.weak).toBe(true);
    expect(form.passphraseMessage).not.toBe('');
    expect(form.usesPassphrase).toBe(true);
    expect(form.nodes[1]?.detail).toBe(new Translator('en').translate('step.derive.detail.passphrase'));
    form.passphrase = 'correct horse battery staple';
    expect(form.passphraseMessage).toBe('');
    await form.toggleProtect();
    expect(form.protect).toBe(false);
    expect(form.passphrase).toBe('');
  });

  it('keeps a passphrase typed while the generated words load', async () => {
    const form = composer();
    const toggled = form.toggleProtect();
    form.passphrase = 'my own long passphrase';
    await toggled;
    expect(form.passphrase).toBe('my own long passphrase');
  });

  it('keeps a passphrase already in the field when protection turns on', async () => {
    const form = composer();
    form.passphrase = 'already typed words here';
    await form.toggleProtect();
    expect(form.passphrase).toBe('already typed words here');
    expect(generatePassphrase).not.toHaveBeenCalled();
  });

  it('leaves the passphrase field empty when the sender reaches it before the generated words load', async () => {
    const form = composer();
    const toggled = form.toggleProtect();
    form.reachPassphrase();
    await toggled;
    expect(form.passphrase).toBe('');
  });

  it('validates file size immediately, removes obsolete errors and resets the file input', () => {
    const form = composer();
    form.problems = [
      { field: 'content', code: 'missing' },
      { field: 'passphrase', code: 'too_short' },
    ];
    form.pickFile(new File([new Uint8Array(17)], 'large.bin'));
    expect(form.problems).toEqual([
      { field: 'passphrase', code: 'too_short' },
      { field: 'file', code: 'too_large' },
    ]);
    form.pickFile(new File(['ok'], 'small.txt'));
    expect(form.problems).toEqual([{ field: 'passphrase', code: 'too_short' }]);
    const input = document.createElement('input');
    input.value = 'selected-file';
    form.focus.attach('file')(input);
    form.removeFile();
    expect(form.file).toBeUndefined();
    expect(input.value).toBe('');
  });

  it('reports unreadable attachments without starting encryption', async () => {
    const form = composer();
    form.file = new File(['ok'], 'missing.txt');
    vi.spyOn(form.file, 'arrayBuffer').mockRejectedValue(new Error('file disappeared'));
    const focused = focusTarget(form, 'file');
    await submit(form);
    expect(form.problems).toEqual([{ field: 'file', code: 'unreadable' }]);
    expect(focused).toHaveBeenCalledOnce();
    expect(createMessage).not.toHaveBeenCalled();
  });

  it('waits for the timeline before showing the link and wipes the temporary attachment bytes', async () => {
    const form = composer();
    form.text = 'synthetic message';
    form.protect = true;
    form.passphrase = 'correct horse battery staple';
    form.opensMax = 3;
    form.file = new File(['ok'], 'message.txt');
    const gate = Promise.withResolvers<Created>();
    vi.mocked(createMessage).mockImplementationOnce((input, _dependencies, progress) => {
      expect(input.file?.bytes).toEqual(new Uint8Array([111, 107]));
      progress?.({ step: 'deriving', fraction: 0.5 });
      progress?.({ step: 'proving', fraction: 0.25 });
      return gate.promise;
    });
    const sending = submit(form);
    await vi.waitFor(() => expect(createMessage).toHaveBeenCalledOnce());
    expect(form.phase).toBe('encrypting');
    expect(form.usesPassphrase).toBe(true);
    expect(form.proving).toBe(true);
    expect(form.timeline.fraction).toBe(0.25);
    expect(form.nodes[3]?.title).toBe(new Translator('en').translate('step.send.title.pow'));
    expect(parseLink(form.placeholder?.link ?? '').kdfId).toBe(KDF_SCRYPT);
    expect(form.placeholder?.expiresAt).toBe(new Date(Date.now() + form.ttlSeconds * 1000).toISOString());
    expect(form.stepTitle).toBe(form.nodes[form.timeline.paced.step]?.title);
    vi.mocked(createMessage).mock.calls[0]?.[2]?.({ step: 'uploading' });
    expect([form.proving, form.timeline.fraction]).toEqual([false, undefined]);
    expect(form.nodes[3]?.title).toBe(new Translator('en').translate('step.send.title'));
    await submit(form);
    expect(createMessage).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ text: 'synthetic message', passphrase: 'correct horse battery staple', opensMax: 3 }),
      expect.objectContaining({ origin: 'https://nylzo.test' }),
      expect.any(Function),
    );
    gate.resolve(created);
    await sending;
    expect(form.text).toBe('');
    expect(form.passphrase).toBe('');
    expect(form.file).toBeUndefined();
    expect(vi.mocked(createMessage).mock.calls[0]?.[0].file?.bytes).toEqual(new Uint8Array(2));
    expect(form.sealed).toBeUndefined();
    vi.advanceTimersByTime(5100);
    expect(form.phase).toBe('done');
    expect(form.sealed).toMatchObject({ created, opensMax: 3, passphrase: 'correct horse battery staple' });
    expect([form.usesPassphrase, form.placeholder, form.stepTitle]).toEqual([true, undefined, '']);
    expect(form.remembered).toBe('off');
    expect(rememberSent).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    const focus = focusTarget(form, 'text');
    await form.another();
    expect(form.phase).toBe('compose');
    expect(form.sealed).toBeUndefined();
    expect(form.protect).toBe(false);
    expect(focus).toHaveBeenCalledOnce();
  });

  it.each([true, false])(
    'saves the manage key in the browser only when the sender asks, and retries a failed save in the tab: stored=%s',
    async (stored) => {
      const form = composer();
      form.text = 'remember this message';
      const gate = Promise.withResolvers<boolean>();
      vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
      await submit(form);
      expect(parseLink(form.placeholder?.link ?? '').kdfId).toBe(KDF_NONE);
      vi.advanceTimersByTime(5100);
      expect(rememberSent).not.toHaveBeenCalled();
      expect([form.usesPassphrase, form.keeps]).toEqual([false, true]);
      const remembering = form.remember('  Heslo pro Petra ');
      expect(rememberSent).toHaveBeenCalledWith(
        browser,
        {
          id: created.id,
          createdAt: Date.now(),
          expiresAt: Date.parse(created.expiresAt),
          label: 'Heslo pro Petra',
          manageKey: created.manageKey,
        },
        5000,
      );
      expect(browserSentStore).toHaveBeenCalledOnce();
      expect(form.remembered).toBe('saving');
      form.canKeep = false;
      expect(form.keeps).toBe(true);
      form.canKeep = true;
      gate.resolve(stored);
      await remembering;
      expect(form.remembered).toBe(stored ? 'remembered' : 'failed');
      expect(form.kept).toBe(true);
      await form.remember('');
      expect(vi.mocked(rememberSent).mock.calls).toHaveLength(stored ? 1 : 2);
      expect(vi.mocked(rememberSent).mock.lastCall?.[0]).toBe(stored ? browser : session);
      expect(form.kept).toBe(stored);
    },
  );

  it('warns before leaving a draft, or a link that was neither copied nor tracked', async () => {
    const form = composer();
    form.text = '  ';
    expect(leaving(form)).toEqual([undefined, false]);
    form.text = 'draft';
    form.passphrase = 'correct horse battery staple';
    expect([...leaving(form), form.passphrase]).toEqual(['draft', true, '']);
    await submit(form);
    vi.advanceTimersByTime(5100);
    expect(leaving(form)).toEqual(['link', true]);
    form.linkCopied = true;
    expect(leaving(form)).toEqual([undefined, false]);
    form.linkCopied = false;
    vi.mocked(rememberSent).mockResolvedValueOnce(false);
    await form.remember('');
    expect(leaving(form)).toEqual(['link', true]);
    await form.remember('');
    expect([form.kept, ...leaving(form)]).toEqual([false, undefined, true]);
  });

  it('keeps a message only in this tab when the page cannot keep it in the browser', async () => {
    const form = composer();
    form.canKeep = false;
    form.text = 'local client';
    await submit(form);
    vi.advanceTimersByTime(5100);
    await form.remember('');
    expect(rememberSent).toHaveBeenCalledExactlyOnceWith(session, expect.objectContaining({ id: created.id }), 5000);
    expect(browserSentStore).not.toHaveBeenCalled();
  });

  it('does not update a reset form when saving a previous record finishes late', async () => {
    const form = composer();
    form.text = 'message';
    const gate = Promise.withResolvers<boolean>();
    vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
    await submit(form);
    vi.advanceTimersByTime(5100);
    const remembering = form.remember('');
    await form.another();
    gate.resolve(true);
    await remembering;
    expect(form.remembered).toBe('off');
    expect(form.sealed).toBeUndefined();
  });

  it.each<CreateFailure>([
    { kind: 'rate_limited', retryAfter: 30 },
    { kind: 'unavailable', retryAfter: 60 },
    { kind: 'kdf' },
    { kind: 'pow' },
    { kind: 'too_large' },
    { kind: 'failed' },
  ])('keeps the draft and focuses submit after an upload failure: $kind', async (failure) => {
    const form = composer();
    form.text = 'draft to retry';
    form.file = new File(['ok'], 'small.txt');
    const focus = focusTarget(form, 'submit');
    vi.mocked(createMessage).mockRejectedValueOnce(new CreateError(failure));
    await submit(form);
    expect(form.failure).toEqual(failure);
    expect(form.notice).not.toBe('');
    expect(form.phase).toBe('compose');
    expect(form.text).toBe('draft to retry');
    expect(form.file.name).toBe('small.txt');
    expect(vi.mocked(createMessage).mock.calls[0]?.[0].file?.bytes).toEqual(new Uint8Array(2));
    expect(focus).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('turns server validation into field errors and handles unexpected upload errors', async () => {
    const form = composer();
    form.text = 'draft';
    const focus = focusTarget(form, 'text');
    vi.mocked(createMessage).mockRejectedValueOnce(
      new CreateError({
        kind: 'invalid',
        problems: [{ field: 'text', code: 'too_large' }],
      }),
    );
    await submit(form);
    expect(form.problems).toEqual([{ field: 'text', code: 'too_large' }]);
    expect(focus).toHaveBeenCalledOnce();
    vi.mocked(createMessage).mockRejectedValueOnce(new Error('unexpected'));
    await submit(form);
    expect(form.failure).toEqual({ kind: 'failed' });
  });

  it.each<[readonly CreateProblem[], string]>([
    [
      [{ field: 'text', code: 'too_large' }],
      new Translator('en').translate('problem.text.too_large', { size: formatBytes(102_400, 'en') }),
    ],
    [
      [
        { field: 'file', code: 'too_large' },
        { field: 'file', code: 'unreadable' },
      ],
      new Translator('en').translate('problem.file.too_large', { size: formatBytes(limits.maxFileBytes, 'en') }),
    ],
    [[{ field: 'ttl', code: 'out_of_range' }], new Translator('en').translate('problem.ttl.out_of_range')],
    [
      [{ field: 'opens', code: 'out_of_range' }],
      new Translator('en').translate('problem.opens.out_of_range', { max: limits.maxOpens }),
    ],
  ])('shows the first problem the instance finds in a field: %j', async (problems, notice) => {
    const form = composer();
    form.text = 'draft';
    vi.mocked(createMessage).mockRejectedValueOnce(new CreateError({ kind: 'invalid', problems }));
    await submit(form);
    expect(form.notice).toBe(notice);
    expect(form.failure).toBeUndefined();
  });

  it.each([
    ['a file without text', '', new File(['ok'], 'notes.txt'), 'notes.txt'],
    ['blank text', '   ', undefined, ''],
  ])('scrambles %s as the cipher of the sealing animation', async (_, text, file, cipher) => {
    const form = composer();
    form.text = text;
    form.file = file;
    vi.mocked(createMessage).mockReturnValueOnce(Promise.withResolvers<Created>().promise);
    void submit(form);
    await vi.waitFor(() => expect(createMessage).toHaveBeenCalledOnce());
    expect(form.cipher).toBe(cipher);
  });

  it.each([false, true])('shows the link sooner when the sender prefers reduced motion: %s', async (reduced) => {
    reducedMotion(reduced);
    const form = composer();
    form.text = 'synthetic message';
    await submit(form);
    vi.advanceTimersByTime(120);
    const cipher = form.cipher;
    vi.advanceTimersByTime(60);
    expect(form.cipher === cipher).toBe(reduced);
    vi.advanceTimersByTime(600);
    expect(form.phase).toBe(reduced ? 'done' : 'encrypting');
    reducedMotion(false);
  });

  it('keeps working when the generated words cannot load', async () => {
    vi.mocked(loadPassphraseWords).mockRejectedValueOnce(new Error('offline'));
    vi.mocked(generatePassphrase).mockRejectedValueOnce(new Error('offline'));
    const form = composer();
    form.toggleMenu('security');
    expect(form.menu).toBe('security');
    await form.toggleProtect();
    expect([form.protect, form.passphrase]).toEqual([true, '']);
  });

  it.each([false, true])('ignores an upload that finishes after leaving, including failure: %s', async (failed) => {
    const form = composer();
    form.text = 'must not reappear';
    form.protect = true;
    form.passphrase = 'correct horse battery staple';
    const gate = Promise.withResolvers<Created>();
    vi.mocked(createMessage).mockReturnValueOnce(gate.promise);
    const sending = submit(form);
    await vi.waitFor(() => expect(createMessage).toHaveBeenCalledOnce());
    form.leave();
    const progress = vi.mocked(createMessage).mock.calls[0]?.[2];
    progress?.({ step: 'proving', fraction: 0.5 });
    if (failed) {
      gate.reject(new Error('late failure'));
    } else {
      gate.resolve(created);
    }
    await sending;
    vi.advanceTimersByTime(10_000);
    expect(form.phase).toBe('compose');
    expect(form.text).toBe('');
    expect(form.passphrase).toBe('');
    expect(form.cipher).toBe('');
    expect(form.sealed).toBeUndefined();
    expect(form.failure).toBeUndefined();
    expect(rememberSent).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('stops a running key derivation when the page is left (SEC-8)', async () => {
    const stop = vi.fn<() => void>();
    const worker = Object.assign(new EventTarget(), { postMessage: vi.fn<(...arguments_: unknown[]) => void>() });
    vi.mocked(startWorker).mockResolvedValue({ worker, stop });
    const form = composer();
    form.text = 'synthetic message';
    form.protect = true;
    form.passphrase = 'correct horse battery staple';
    let derivation: Promise<Uint8Array<ArrayBuffer>> | undefined;
    vi.mocked(createMessage).mockImplementationOnce(async (_input, dependencies) => {
      derivation = dependencies.stretch(new Uint8Array(64), vi.fn<(fraction: number) => void>());
      return derivation.then(() => created);
    });
    const sending = submit(form);
    await vi.waitFor(() => expect(worker.postMessage).toHaveBeenCalledOnce());
    form.leave();
    await expect(derivation).rejects.toBeInstanceOf(KdfWorkerError);
    expect(stop).toHaveBeenCalledOnce();
    await sending;
  });

  it('ignores an attachment read that finishes after leaving and prevents overlapping submissions', async () => {
    const form = composer();
    form.file = new File(['ok'], 'slow.txt');
    const gate = Promise.withResolvers<ArrayBuffer>();
    const read = vi.spyOn(form.file, 'arrayBuffer').mockReturnValue(gate.promise);
    const sending = submit(form);
    await submit(form);
    expect(read).toHaveBeenCalledOnce();
    form.leave();
    const bytes = new Uint8Array([1, 2]);
    gate.resolve(bytes.buffer);
    await sending;
    expect(bytes).toEqual(new Uint8Array(2));
    expect(createMessage).not.toHaveBeenCalled();
  });

  it('toggles menus, keeps inside clicks open and reloads only a restored document', () => {
    const form = composer();
    form.toggleMenu('ttl');
    expect(form.menu).toBe('ttl');
    const menu = document.createElement('div');
    const item = document.createElement('button');
    menu.dataset['menu'] = 'ttl';
    menu.append(item);
    const inside = new MouseEvent('click');
    item.dispatchEvent(inside);
    form.onWindowClick(inside);
    expect(form.menu).toBe('ttl');
    form.onWindowClick(new MouseEvent('click'));
    expect(form.menu).toBeUndefined();
    form.toggleMenu('opens');
    form.toggleMenu('opens');
    expect(form.menu).toBeUndefined();
    const focus = focusTarget(form, 'security');
    form.toggleMenu('security');
    form.closeMenu('security');
    expect(focus).toHaveBeenCalledOnce();
    form.returned(Object.assign(new Event('pageshow'), { persisted: false }));
    expect(reload).not.toHaveBeenCalled();
    form.returned(Object.assign(new Event('pageshow'), { persisted: true }));
    expect(reload).toHaveBeenCalledOnce();
  });
});

function leaving(form: Composer): readonly [Composer['unsaved'], boolean] {
  const event = new Event('beforeunload', { cancelable: true });
  form.guardLeaving(event);
  return [form.unsaved, event.defaultPrevented];
}

function submit(form: Composer): Promise<void> {
  return form.submit(new SubmitEvent('submit', { cancelable: true }));
}

function focusTarget(form: Composer, target: Parameters<typeof form.focus.attach>[0]): Mock<HTMLElement['focus']> {
  const focus = vi.fn<HTMLElement['focus']>();
  form.focus.attach(target)(Object.assign(document.createElement('button'), { focus }));
  return focus;
}
