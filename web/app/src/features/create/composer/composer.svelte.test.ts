import { requestKey } from '@nylzo/core';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { InstanceLimits } from '../../../shared/api/responses.ts';

import { createApi } from '../../../shared/api/api.ts';
import { fakeApi } from '../../../shared/api/fake-api.ts';
import { Translator } from '../../../shared/language/translator.svelte.ts';
import { memoryStore } from '../../../shared/sent-records/memory-sent-store.ts';
import { browserSentStore, rememberSent } from '../../../shared/sent-records/sent-store.ts';
import { type Created, CreateError, type CreateFailure, createMessage } from '../create-message.ts';
import { Composer } from './composer.svelte.ts';

vi.mock('../../../shared/api/api.ts', () => ({ createApi: vi.fn() }));
vi.mock('../../../shared/sent-records/sent-store.ts', () => ({
  browserSentStore: vi.fn(),
  rememberSent: vi.fn(),
}));
// Keep validation real; only replace the encryption/upload boundary.
vi.mock(import('../create-message.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Await instrumentation cannot run before imports initialize.
  original().then((actual) => ({ ...actual, createMessage: vi.fn() })),
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

  function composer(): Composer {
    const form = new Composer(new Translator('en'));
    form.limits = limits;
    composers.push(form);
    return form;
  }

  function submit(form: Composer): Promise<void> {
    return form.submit(new Event('submit', { cancelable: true }) as SubmitEvent);
  }

  function focusTarget(form: Composer, target: Parameters<typeof form.focus.attach>[0]): ReturnType<typeof vi.fn> {
    const focus = vi.fn();
    form.focus.attach(target)({ focus } as unknown as HTMLElement);
    return focus;
  }

  const querySelector = vi.fn();
  const reload = vi.fn();

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
    vi.mocked(browserSentStore).mockReturnValue(memoryStore());
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

  it('disables persistent records when served from the verified local client', () => {
    querySelector.mockReturnValue({ content: 'https://instance.test' });
    const form = composer();
    form.remember = true;
    const stop = form.start({} as HTMLElement);
    expect(form.canRemember).toBe(false);
    expect(form.remember).toBe(false);
    if (typeof stop === 'function') {
      stop();
    }
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
    expect(form.passphraseVisible).toBe(true);
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
    expect(form.passphraseVisible).toBe(false);
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
    form.passphraseVisible = true;
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
    expect(createMessage).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'synthetic message', passphrase: 'correct horse battery staple', opensMax: 3 }),
      expect.objectContaining({ origin: 'https://nylzo.test' }),
      expect.any(Function),
    );
    expect(form.phase).toBe('encrypting');
    expect(form.usesPassphrase).toBe(true);
    expect(form.proving).toBe(true);
    expect(form.timeline.fraction).toBe(0.25);
    expect(form.nodes[3]?.title).toBe(new Translator('en').translate('step.send.title.pow'));
    await submit(form);
    expect(createMessage).toHaveBeenCalledOnce();
    gate.resolve(created);
    await sending;
    expect(form.text).toBe('');
    expect(form.passphrase).toBe('');
    expect(form.passphraseVisible).toBe(false);
    expect(form.file).toBeUndefined();
    expect(vi.mocked(createMessage).mock.calls[0]?.[0].file?.bytes).toEqual(new Uint8Array(2));
    expect(form.sealed).toBeUndefined();
    vi.advanceTimersByTime(5100);
    expect(form.phase).toBe('done');
    expect(form.sealed).toMatchObject({ created, opensMax: 3, passphrase: true });
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

  it.each([true, false])('shows whether the manage key was saved after the link appears: %s', async (stored) => {
    const form = composer();
    form.text = 'remember this message';
    form.remember = true;
    const gate = Promise.withResolvers<boolean>();
    vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
    await submit(form);
    expect(rememberSent).toHaveBeenCalledWith(
      expect.anything(),
      { id: created.id, createdAt: Date.now(), expiresAt: Date.parse(created.expiresAt), manageKey: created.manageKey },
      5000,
    );
    vi.advanceTimersByTime(5100);
    expect(form.remembered).toBe('saving');
    gate.resolve(stored);
    await gate.promise;
    await Promise.resolve();
    expect(form.remembered).toBe(stored ? 'remembered' : 'failed');
  });

  it('does not update a reset form when saving a previous record finishes late', async () => {
    const form = composer();
    form.text = 'message';
    form.remember = true;
    const gate = Promise.withResolvers<boolean>();
    vi.mocked(rememberSent).mockReturnValueOnce(gate.promise);
    await submit(form);
    vi.advanceTimersByTime(5100);
    await form.another();
    gate.resolve(true);
    await gate.promise;
    await Promise.resolve();
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
