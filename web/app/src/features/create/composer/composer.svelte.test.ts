import { requestKey } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { createApi } from '../../../shared/api/api.ts';
import { fakeApi } from '../../../shared/api/fake-api.ts';
import type { InstanceLimits } from '../../../shared/api/responses.ts';
import { KdfWorkerError } from '../../../shared/kdf/kdf-protocol.ts';
import { startWorker } from '../../../shared/kdf/worker-source.ts';
import { Translator } from '../../../shared/language/translator.svelte.ts';
import { memoryStore } from '../../../shared/sent-records/memory-sent-store.ts';
import { browserSentStore, rememberSent } from '../../../shared/sent-records/sent-store.ts';
import { CreateError, createMessage } from '../create-message.ts';
import type { Created, CreateFailure } from '../create-message.ts';
import { Composer } from './composer.svelte.ts';

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

class TestInput extends Element {
  value = 'selected-file';
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

  const querySelector = vi.fn<() => { content: string } | null>();
  const reload = vi.fn<(...arguments_: unknown[]) => void>();

  beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    querySelector.mockReturnValue(null);
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
    vi.stubGlobal('document', { querySelector });
    vi.stubGlobal('location', { origin: 'https://nylzo.test', reload });
    vi.stubGlobal('HTMLInputElement', TestInput);
  });

  afterEach(() => {
    for (const form of composers) {
      form.leave();
    }
    vi.useRealTimers();
    vi.unstubAllGlobals();
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
    expect(form.fileLimit).toBe(16);
  });

  it('tracks only in the tab, never in browser storage, when served from the verified local client', () => {
    querySelector.mockReturnValue({ content: 'https://instance.test' });
    const form = composer();
    const stop = form.start({} as HTMLElement);
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
    expect(createMessage).not.toHaveBeenCalled();
    form.generatePassphrase();
    expect(form.problems).toEqual([]);
    form.forgetPassphrase();
    expect(form.passphrase).toBe('');
    form.passphrase = 'password';
    expect(form.weak).toBe(true);
    expect(form.passphraseMessage).not.toBe('');
    await form.toggleProtect();
    expect(form.protect).toBe(false);
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
    const input = new TestInput();
    form.focus.attach('file')(input as unknown as HTMLElement);
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

  it.each([
    [true, true],
    [true, false],
    [false, true],
  ])(
    'saves the manage key only when the sender asks, in the browser only when kept, and shows the result: kept=%s stored=%s',
    async (keep, stored) => {
      const form = composer();
      form.text = 'remember this message';
      const gate = Promise.withResolvers<boolean>();
      vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
      await submit(form);
      vi.advanceTimersByTime(5100);
      expect(rememberSent).not.toHaveBeenCalled();
      const remembering = form.remember('  Heslo pro Petra ', keep);
      expect(rememberSent).toHaveBeenCalledWith(
        keep ? browser : session,
        {
          id: created.id,
          createdAt: Date.now(),
          expiresAt: Date.parse(created.expiresAt),
          label: 'Heslo pro Petra',
          manageKey: created.manageKey,
        },
        5000,
      );
      expect(browserSentStore).toHaveBeenCalledTimes(keep ? 1 : 0);
      expect(form.remembered).toBe('saving');
      gate.resolve(stored);
      await remembering;
      expect(form.remembered).toBe(stored ? 'remembered' : 'failed');
      expect(form.kept).toBe(keep);
      await form.remember('', true);
      expect(vi.mocked(rememberSent).mock.calls).toHaveLength(1);
      await form.remember('', false);
      expect(vi.mocked(rememberSent).mock.calls).toHaveLength(keep && !stored ? 2 : 1);
    },
  );

  it('keeps a message only in this tab when the page cannot keep it in the browser', async () => {
    const form = composer();
    form.canKeep = false;
    form.text = 'local client';
    await submit(form);
    vi.advanceTimersByTime(5100);
    await form.remember('', true);
    expect(rememberSent).not.toHaveBeenCalled();
    await form.remember('', false);
    expect(rememberSent).toHaveBeenCalledWith(session, expect.objectContaining({ id: created.id }), 5000);
  });

  it('does not update a reset form when saving a previous record finishes late', async () => {
    const form = composer();
    form.text = 'message';
    const gate = Promise.withResolvers<boolean>();
    vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
    await submit(form);
    vi.advanceTimersByTime(5100);
    const remembering = form.remember('', true);
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
    vi.mocked(startWorker).mockResolvedValue({ worker: worker as unknown as Worker, stop });
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
    const menu = Object.assign(new Element(), { closest: () => ({}) });
    form.onWindowClick({ target: menu } as unknown as MouseEvent);
    expect(form.menu).toBe('ttl');
    form.onWindowClick({ target: null } as MouseEvent);
    expect(form.menu).toBeUndefined();
    form.toggleMenu('opens');
    form.toggleMenu('opens');
    expect(form.menu).toBeUndefined();
    const focus = focusTarget(form, 'security');
    form.toggleMenu('security');
    form.closeMenu('security');
    expect(focus).toHaveBeenCalledOnce();
    form.returned({ persisted: false } as PageTransitionEvent);
    expect(reload).not.toHaveBeenCalled();
    form.returned({ persisted: true } as PageTransitionEvent);
    expect(reload).toHaveBeenCalledOnce();
  });
});

function submit(form: Composer): Promise<void> {
  return form.submit(new Event('submit', { cancelable: true }) as SubmitEvent);
}

function focusTarget(form: Composer, target: Parameters<typeof form.focus.attach>[0]): ReturnType<typeof vi.fn> {
  const focus = vi.fn<(...arguments_: unknown[]) => void>();
  form.focus.attach(target)({ focus, scrollIntoView: vi.fn<() => void>() } as unknown as HTMLElement);
  return focus;
}
