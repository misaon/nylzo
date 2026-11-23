import { KDF_NONE, KDF_SCRYPT } from '@nylzo/core';
import { DetachedWindowAPI } from 'happy-dom';
import { flushSync, onDestroy } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Metadata } from '../../shared/api/api.ts';
import { revokeSavedFiles, saveFile } from '../../shared/files/save-file.ts';
import { KdfWorkerError } from '../../shared/kdf/kdf-protocol.ts';
import { startWorker } from '../../shared/kdf/worker-source.ts';
import { Translator } from '../../shared/language/translator.svelte.ts';
import { takeFragment } from '../../shared/link/link-handoff.ts';
import { openedFacts } from './read-facts.ts';
import { burnText, noticeOf } from './read-notice.ts';
import { ReadPage, SHOWN_SECONDS } from './read-page.svelte.ts';
import type { BurnOutcome, ClosedView, ReaderDependencies, ReadView } from './reader.ts';
import { createReader } from './reader.ts';

// Avoid an await in this hoisted factory: Svelte instruments awaits with a runtime import.
vi.mock(import('svelte'), (original) =>
  original().then((actual) => ({ ...actual, onDestroy: vi.fn<typeof actual.onDestroy>() })),
);
vi.mock('./reader.ts', () => ({ createReader: vi.fn<typeof import('./reader.ts').createReader>() }));
vi.mock('../../shared/files/save-file.ts', () => ({
  revokeSavedFiles: vi.fn<typeof import('../../shared/files/save-file.ts').revokeSavedFiles>(),
  saveFile: vi.fn<typeof import('../../shared/files/save-file.ts').saveFile>(),
}));
vi.mock('../../shared/kdf/worker-source.ts', () => ({
  prepareWorker: vi.fn<typeof import('../../shared/kdf/worker-source.ts').prepareWorker>(),
  startWorker: vi.fn<typeof import('../../shared/kdf/worker-source.ts').startWorker>(),
}));
vi.mock('../../shared/link/link-handoff.ts', () => ({
  takeFragment: vi.fn<typeof import('../../shared/link/link-handoff.ts').takeFragment>(),
}));

const metadata: Metadata = {
  state: 'available',
  opensMax: 3,
  opensRemaining: 2,
  ttlSeconds: 3600,
  kdfId: KDF_NONE,
  size: 100,
  expiresAt: '2026-11-01T13:00:00Z',
};
const file = { name: 'message.pdf', mime: 'application/pdf', bytes: new Uint8Array([1, 2, 3]) };
const pageDocument = document;

function reducedMotion(matches: boolean): void {
  const environment: object = globalThis;
  if (!('happyDOM' in environment) || !(environment.happyDOM instanceof DetachedWindowAPI)) {
    throw new TypeError('these tests need the happy-dom environment');
  }
  environment.happyDOM.settings.device.prefersReducedMotion = matches ? 'reduce' : 'no-preference';
  dispatchEvent(new Event('resize'));
  flushSync();
}

const reader = {
  load: vi.fn<() => Promise<void>>(),
  reveal: vi.fn<(passphrase?: string) => Promise<void>>(),
  burn: vi.fn<() => Promise<BurnOutcome>>(),
  clear: vi.fn<(closed?: ClosedView) => void>(),
};
let notify: (view: ReadView) => void;
let dependencies: ReaderDependencies;
let destroy: (() => void)[];

function ready(overrides: Partial<Metadata> = {}): void {
  notify({ view: 'ready', metadata: { ...metadata, ...overrides }, resumable: false, deviceFailed: false });
}

const writeText = vi.fn<(text: string) => Promise<void>>();
const reload = vi.fn<(...arguments_: unknown[]) => void>();

function prepareReadPage(): void {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    destroy = [];
    vi.mocked(onDestroy).mockImplementation((callback) => {
      destroy.push(() => {
        callback();
      });
    });
    vi.mocked(takeFragment).mockReturnValue({ fragment: '#secret', handedOver: false });
    reader.load.mockResolvedValue();
    reader.reveal.mockResolvedValue();
    reader.burn.mockResolvedValue({ outcome: 'deleted' });
    reader.clear.mockImplementation((closed) => notify(closed ?? { view: 'cleared' }));
    vi.mocked(createReader).mockImplementation((options, onView) => {
      dependencies = options;
      notify = onView;
      return reader;
    });
    vi.stubGlobal('document', {
      activeElement: null,
      body: {},
      visibilityState: 'visible',
      querySelector: () => null,
    });
    vi.stubGlobal('location', { pathname: '/m/example', hash: '', origin: 'https://nylzo.test', reload });
    vi.stubGlobal('sessionStorage', {
      getItem: vi.fn<(...arguments_: unknown[]) => void>(),
      setItem: vi.fn<(...arguments_: unknown[]) => void>(),
      removeItem: vi.fn<(...arguments_: unknown[]) => void>(),
    });
    writeText.mockResolvedValue();
    vi.stubGlobal('navigator', { clipboard: { writeText } });
  });

  afterEach(() => {
    for (const callback of destroy) {
      callback();
    }
    reducedMotion(false);
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
}

describe('read page interactions', () => {
  prepareReadPage();

  it('loads once and records whether the secret link was already in browser history', () => {
    const page = new ReadPage(new Translator('en'));
    expect(reader.load).toHaveBeenCalledExactlyOnceWith();
    expect(page.status).toBe(new Translator('en').translate('read.loading'));
    expect(dependencies.takeFragment()).toBe('#secret');
    expect(takeFragment).toHaveBeenCalledOnce();
    expect(page.linkInHistory).toBe(true);
    vi.mocked(takeFragment).mockReturnValue({ fragment: '#handed', handedOver: true });
    dependencies.takeFragment();
    expect(page.linkInHistory).toBe(false);
    page.retry();
    expect(reader.load).toHaveBeenCalledTimes(2);
    expect(dependencies.page.visible()).toBe(true);
    expect(dependencies.page.prerendering()).toBe(false);
    expect(dependencies.now()).toBe(Date.now());
  });

  it('keeps the lease of a local client session in memory, never in session storage (Protocol §14.1)', () => {
    vi.stubGlobal('location', { pathname: '/r', hash: '', origin: 'http://abc.localhost:8095', reload });
    vi.stubGlobal('document', {
      activeElement: null,
      body: {},
      visibilityState: 'visible',
      querySelector: () => ({ content: 'https://nylzo.example' }),
    });
    const setItem = vi.fn<(key: string, value: string) => void>();
    vi.stubGlobal('sessionStorage', { getItem: () => null, setItem, removeItem: setItem });
    void new ReadPage(new Translator('en'));
    dependencies.session.setItem('nylzo:lease:r', 'kept');
    expect(dependencies.session.getItem('nylzo:lease:r')).toBe('kept');
    dependencies.session.removeItem('nylzo:lease:r');
    expect(dependencies.session.getItem('nylzo:lease:r')).toBeNull();
    expect(setItem).not.toHaveBeenCalled();
  });

  it('requires a passphrase before revealing and clears it before handing it to the reader', async () => {
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    const focus = focusTarget(page, 'passphrase');
    const event = new SubmitEvent('submit', { cancelable: true });
    page.reveal(event);
    expect(event.defaultPrevented).toBe(true);
    expect(page.emptyPassphrase).toBe(true);
    expect(focus).toHaveBeenCalledOnce();
    expect(reader.reveal).not.toHaveBeenCalled();
    page.passphrase = 'correct horse battery staple';
    page.showPassphrase = true;
    page.typed();
    expect(page.emptyPassphrase).toBe(false);
    expect(page.typedAgain).toBe(true);
    const gate = Promise.withResolvers<undefined>();
    reader.reveal.mockReturnValueOnce(gate.promise);
    page.reveal(event);
    page.reveal(event);
    expect(reader.reveal).toHaveBeenCalledExactlyOnceWith('correct horse battery staple');
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    gate.resolve(undefined);
    await gate.promise;
  });

  it('reveals once when the passphrase was submitted while the message was still loading', () => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'loading', kdfId: KDF_SCRYPT });
    page.passphrase = 'correct horse battery staple';
    const event = new SubmitEvent('submit', { cancelable: true });
    page.reveal(event);
    page.reveal(event);
    expect(reader.reveal).not.toHaveBeenCalled();
    ready({ kdfId: KDF_SCRYPT });
    expect(reader.reveal).toHaveBeenCalledExactlyOnceWith('correct horse battery staple');
  });

  it('holds the revealed content until the opening timeline finishes, then unscrambles it', async () => {
    const page = new ReadPage(new Translator('en'));
    ready();
    const focused = focusTarget(page, 'revealedTitle');
    notify({ view: 'busy', step: 'claiming' });
    expect(page.phase).toBe('opening');
    expect(page.actions).toBe('dim');
    notify({ view: 'busy', step: 'downloading', fraction: 0.5 });
    expect(page.timeline.fraction).toBe(0.5);
    expect(page.status).toBe(new Translator('en').translate('busy.downloading'));
    notify({ view: 'revealed', text: 'Hello 🌍\nworld', burnable: true, opensRemaining: 1 });
    const displayed = vi.fn<(...arguments_: unknown[]) => void>();
    const shown = (async () => {
      await dependencies.shown?.();
      displayed();
    })();
    vi.advanceTimersByTime(800);
    expect(page.revealed).toBeUndefined();
    expect(displayed).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2000);
    await shown;
    expect(page.phase).toBe('revealed');
    expect([page.revealHead, page.revealRest]).toEqual(['Hello 🌍\nworld', '']);
    expect(focused).toHaveBeenCalledOnce();
    expect(page.secondsLeft).toBeGreaterThan(SHOWN_SECONDS - 5);
    expect(vi.getTimerCount()).toBe(1);
  });

  it('uses a shorter timeline and skips scrambling for reduced motion', async () => {
    reducedMotion(true);
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    expect(page.steps).toHaveLength(4);
    notify({ view: 'busy', step: 'deriving' });
    notify({ view: 'revealed', text: 'Private text', burnable: false, opensRemaining: 0 });
    await vi.advanceTimersByTimeAsync(1100);
    expect([page.revealHead, page.revealRest]).toEqual(['Private text', '']);
    expect(page.phase).toBe('revealed');
    expect(page.secondsLeft).toBe(SHOWN_SECONDS);
    expect(vi.getTimerCount()).toBe(1);
    await expect(dependencies.shown?.()).resolves.toBeUndefined();
  });

  it('returns to the passphrase field after a rejected attempt and blocks a locked message', async () => {
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    const focus = focusTarget(page, 'passphrase');
    notify({ view: 'busy', step: 'deriving' });
    notify({
      view: 'ready',
      metadata: { ...metadata, kdfId: KDF_SCRYPT },
      resumable: false,
      deviceFailed: false,
      notice: { reason: 'wrong', attemptsLeft: 0, retryAfter: 30, locked: true },
    });
    await Promise.resolve();
    expect(focus).toHaveBeenCalledOnce();
    expect(page.phase).toBe('notice');
    expect(page.notice).toBe('locked');
    expect(page.actions).toBe('off');
    page.reveal(new SubmitEvent('submit'));
    expect(reader.reveal).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('asks before every download, a file named like a PDF included', async () => {
    const page = new ReadPage(new Translator('en'));
    const click = new MouseEvent('click', { detail: 1 });
    const doubleClick = new MouseEvent('click', { detail: 2 });
    const keep = focusTarget(page, 'keepFile');
    await page.download.ask(doubleClick);
    expect(page.download.shown).toBe(false);
    await page.download.ask(click);
    expect(page.download.shown).toBe(true);
    expect(keep).toHaveBeenCalledOnce();
    expect(saveFile).not.toHaveBeenCalled();
    await page.download.confirm(click, file);
    expect(saveFile).not.toHaveBeenCalled();
    vi.advanceTimersByTime(999);
    expect(page.download.ready).toBe(false);
    vi.advanceTimersByTime(1);
    expect(page.download.ready).toBe(true);
    await page.download.confirm(doubleClick, file);
    expect(saveFile).not.toHaveBeenCalled();
    vi.mocked(saveFile).mockImplementationOnce(() => {
      throw new Error('out of memory');
    });
    await expect(page.download.confirm(click, file)).rejects.toThrow('out of memory');
    expect(page.download.shown).toBe(true);
    await page.download.confirm(click, file);
    expect(saveFile).toHaveBeenLastCalledWith(file.bytes, file.name);
    expect(saveFile).toHaveBeenCalledTimes(2);
    expect(page.download.shown).toBe(false);
    expect(page.download.ready).toBe(false);
    expect(page.downloaded.shown).toBe(true);
    await page.download.confirm(click, file);
    expect(saveFile).toHaveBeenCalledTimes(2);
    await page.download.ask(click);
    await page.download.cancel(doubleClick);
    expect(page.download.shown).toBe(true);
    await page.download.cancel(click);
    expect(page.download.shown).toBe(false);
    await page.download.ask(click);
    page.download.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));
    expect(page.download.shown).toBe(true);
    const escape = new KeyboardEvent('keydown', { key: 'Escape' });
    const stopPropagation = vi.spyOn(escape, 'stopPropagation');
    page.download.onKeydown(escape);
    expect(stopPropagation).toHaveBeenCalledOnce();
    expect(page.download.shown).toBe(false);
    vi.advanceTimersByTime(2400);
    expect(page.download.ready).toBe(false);
    expect(page.downloaded.shown).toBe(false);
  });

  it('warns before copying hidden characters and copies the original text only after confirmation', async () => {
    reducedMotion(true);
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text: 'hello\u{200B}world', burnable: false, opensRemaining: 0 });
    await page.askCopy();
    expect(page.confirmingCopy).toBe(true);
    expect(writeText).not.toHaveBeenCalled();
    page.cancelCopy();
    expect(page.confirmingCopy).toBe(false);
    await page.askCopy();
    await page.copyText();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('hello\u{200B}world');
    expect(page.copied.shown).toBe(true);
    expect(page.copyFailed.shown).toBe(false);
  });

  it('handles clipboard refusal without a false success indication', async () => {
    const page = new ReadPage(new Translator('en'));
    await page.askCopy();
    expect(page.confirmingCopy).toBe(false);
    await page.copyText();
    expect(writeText).not.toHaveBeenCalled();
    notify({ view: 'revealed', text: 'hello', burnable: false, opensRemaining: 0 });
    vi.mocked(writeText).mockRejectedValueOnce(new Error('clipboard denied'));
    await page.copyText();
    expect(page.copied.shown).toBe(false);
    expect(page.copyFailed.shown).toBe(true);
    await page.askCopy();
    expect(page.copied.shown).toBe(true);
    expect(page.copyFailed.shown).toBe(false);
    vi.mocked(writeText).mockRejectedValue(new Error('clipboard denied'));
    await page.copyText();
    expect(page.copied.shown).toBe(false);
    expect(page.copyFailed.shown).toBe(true);
  });

  it.each<BurnOutcome>([
    { outcome: 'deleted' },
    { outcome: 'ended', state: 'expired' },
    { outcome: 'failed' },
    { outcome: 'retry_later', retryAfter: 30 },
    { outcome: 'unknown' },
    { outcome: 'ignored' },
    { outcome: 'waiting', retryAfter: 60, locked: false },
    { outcome: 'waiting', retryAfter: 86_400, locked: true },
  ])('preserves the content and reports the result of deleting remaining opens: $outcome', async (outcome) => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text: 'keep on screen', burnable: true, opensRemaining: 1 });
    const gate = Promise.withResolvers<BurnOutcome>();
    reader.burn.mockReturnValueOnce(gate.promise);
    const burning = page.burn();
    expect(page.deleting).toBe(true);
    gate.resolve(outcome);
    await burning;
    expect(page.deleting).toBe(false);
    expect(page.revealed?.text).toBe('keep on screen');
    expect(page.restDeleted).toBe(outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    expect(page.burnFailed).toBe(['failed', 'retry_later', 'unknown', 'waiting'].includes(outcome.outcome));
    expect(page.burnNote === '').toBe(outcome.outcome === 'ignored');
    expect(page.burned).toEqual({ outcome, afterReveal: true });
  });

  it('asks for the passphrase before deleting a message that has one and hands it to the reader', async () => {
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    const focus = focusTarget(page, 'passphrase');
    await page.askDelete();
    expect(page.emptyPassphrase).toBe(true);
    expect(page.askingDelete).toBe(false);
    expect(focus).toHaveBeenCalledOnce();
    page.passphrase = 'correct horse battery staple';
    page.typed();
    page.reveal(new SubmitEvent('submit', { cancelable: true }));
    expect(page.askingDelete).toBe(true);
    page.reveal(new SubmitEvent('submit', { cancelable: true, submitter: pageDocument.createElement('button') }));
    expect(reader.reveal).not.toHaveBeenCalled();
    expect(page.deleteIntent).toBe(true);
    page.passphrase = '';
    await page.burn();
    expect(reader.burn).not.toHaveBeenCalled();
    expect(page.askingDelete).toBe(false);
    page.passphrase = 'correct horse battery staple';
    await page.askDelete();
    page.showPassphrase = true;
    const gate = Promise.withResolvers<BurnOutcome>();
    reader.burn.mockReturnValueOnce(gate.promise);
    const burning = page.burn();
    expect(page.burnNote).toBe(new Translator('en').translate('busy.deriving'));
    await page.askDelete();
    expect(page.askingDelete).toBe(false);
    gate.resolve({ outcome: 'rejected' });
    await burning;
    expect(reader.burn).toHaveBeenCalledExactlyOnceWith('correct horse battery staple');
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    expect(page.deleteIntent).toBe(true);
    expect(page.burnNote).toBe('');
    expect(page.burnFailed).toBe(false);
    expect(focus).toHaveBeenCalledTimes(3);
  });

  it('moves focus safely through deletion confirmation and restores actions when cancelled', async () => {
    const page = new ReadPage(new Translator('en'));
    ready();
    const keep = focusTarget(page, 'keepMessage');
    const remove = focusTarget(page, 'deleteMessage');
    await page.askDelete();
    expect(page.actions).toBe('off');
    expect(keep).toHaveBeenCalledOnce();
    page.onConfirmKeydown(new KeyboardEvent('keydown'));
    expect(page.askingDelete).toBe(true);
    const escape = new KeyboardEvent('keydown', { key: 'Escape' });
    const stopPropagation = vi.spyOn(escape, 'stopPropagation');
    page.onConfirmKeydown(escape);
    await Promise.resolve();
    expect(stopPropagation).toHaveBeenCalledOnce();
    expect(page.askingDelete).toBe(false);
    expect(page.actions).toBe('on');
    expect(remove).toHaveBeenCalledOnce();
  });

  it('forgets an opened message once it was on screen for the shown time', () => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text: 'secret', file, burnable: false, opensRemaining: 0 });
    expect(page.afterFacts[0]).toEqual({ icon: 'timer', text: 'Deleted in 15:00' });
    vi.advanceTimersByTime(61_000);
    expect(page.afterFacts[0]?.text).toBe('Deleted in 13:59');
    notify({ view: 'revealed', text: 'secret', file, burnable: false, opensRemaining: 0 });
    expect(page.afterFacts[0]?.text).toBe('Deleted in 13:59');
    expect(reader.clear).not.toHaveBeenCalled();
    vi.advanceTimersByTime((SHOWN_SECONDS - 61) * 1000);
    expect(reader.clear).toHaveBeenCalledExactlyOnceWith({ view: 'timed_out' });
    expect(page.notice).toBe('timed_out');
    expect(page.status).toBe('Time to view is up');
    expect(page.revealHead).toBeUndefined();
    expect(page.secondsLeft).toBeUndefined();
    expect(revokeSavedFiles).toHaveBeenCalledOnce();
  });

  it('forgets an opened message when the device slept past the shown time', () => {
    const page = new ReadPage(new Translator('en'));
    page.checkCountdown();
    expect(reader.clear).not.toHaveBeenCalled();
    notify({ view: 'revealed', text: 'secret', file, burnable: false, opensRemaining: 0 });
    const shown = Date.now();
    vi.spyOn(Date, 'now').mockReturnValue(shown + (SHOWN_SECONDS + 1) * 1000);
    page.checkCountdown();
    expect(reader.clear).toHaveBeenCalledExactlyOnceWith({ view: 'timed_out' });
    expect(page.revealHead).toBeUndefined();
  });

  it('shows metadata and end-state facts in the selected language', () => {
    const translator = new Translator('cs');
    const page = new ReadPage(translator);
    expect(page.readyFacts).toEqual([]);
    ready({ kdfId: KDF_SCRYPT });
    expect(page.readyFacts.map((fact) => fact.icon)).toEqual(['clock', 'eye', 'key']);
    const { state, opensMax, opensRemaining, ttlSeconds, kdfId, size } = metadata;
    notify({
      view: 'ready',
      metadata: { state, opensMax, opensRemaining, ttlSeconds, kdfId, size },
      resumable: false,
      deviceFailed: false,
    });
    expect(page.readyFacts.map((fact) => fact.icon)).toEqual(['eye']);
    expect(page.opensLeft(1)).toBe('Zbývá 1 otevření');
    expect(page.opensLeft(2)).toBe('Zbývají 2 otevření');
    expect(page.opensLeft(5)).toBe('Zbývá 5 otevření');
    notify({ view: 'ended', state: 'opened', firstOpenedAt: metadata.expiresAt });
    expect(page.afterFacts[0]?.icon).toBe('eye');
    notify({ view: 'ended', state: 'expired', endedAt: metadata.expiresAt });
    expect(page.afterFacts[0]?.icon).toBe('clock');
    notify({ view: 'ended', state: 'deleted' });
    page.burned = { outcome: { outcome: 'deleted' }, afterReveal: false };
    expect(page.notice).toBe('burned');
    expect(page.afterFacts[0]?.text).toBe(translator.translate('open.chip.burned'));
  });

  it('wipes page state on leave, stops key derivation, releases downloads, and stops all animation', async () => {
    const stop = vi.fn<() => void>();
    const worker = Object.assign(new EventTarget(), { postMessage: vi.fn<(...arguments_: unknown[]) => void>() });
    vi.mocked(startWorker).mockResolvedValue({ worker, stop });
    const page = new ReadPage(new Translator('en'));
    const derivation = dependencies.stretch(new Uint8Array(64), vi.fn<(fraction: number) => void>());
    await vi.waitFor(() => expect(worker.postMessage).toHaveBeenCalledOnce());
    ready();
    notify({ view: 'busy', step: 'claiming' });
    notify({ view: 'revealed', text: 'must not reappear', burnable: true, opensRemaining: 1 });
    const displayed = dependencies.shown?.();
    page.passphrase = 'secret';
    page.showPassphrase = true;
    page.confirmingCopy = true;
    page.download.shown = true;
    page.askingDelete = true;
    page.leave();
    await displayed;
    await expect(derivation).rejects.toBeInstanceOf(KdfWorkerError);
    expect(stop).toHaveBeenCalledOnce();
    expect(revokeSavedFiles).toHaveBeenCalledOnce();
    vi.advanceTimersByTime(10_000);
    expect(reader.clear).toHaveBeenCalledOnce();
    expect(page.shown).toEqual({ view: 'cleared' });
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    expect(page.confirmingCopy).toBe(false);
    expect(page.download.shown).toBe(false);
    expect(page.askingDelete).toBe(false);
    expect(page.revealHead).toBeUndefined();
    expect(page.marked).toBeUndefined();
    expect(page.opening).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('starts the passphrase shown where it needs an input method and returns to that after forgetting it', () => {
    const page = new ReadPage(new Translator('ja'));
    expect(page.showPassphrase).toBe(true);
    page.showPassphrase = false;
    expect(page.showPassphrase).toBe(false);
    page.passphrase = 'さくら';
    page.forgetPassphrase();
    expect(page.showPassphrase).toBe(true);
  });

  it('keeps what the reader typed shown or masked when the page language changes', () => {
    const translator = new Translator('zh-Hans');
    const page = new ReadPage(translator);
    page.passphrase = '月亮';
    page.typed();
    translator.language = 'en';
    expect(page.showPassphrase).toBe(true);
    page.forgetPassphrase();
    expect(page.showPassphrase).toBe(false);
  });

  it('forgets a visible passphrase and reloads restored documents or newly pasted fragments', () => {
    const page = new ReadPage(new Translator('en'));
    page.passphrase = 'secret';
    page.showPassphrase = true;
    page.forgetPassphrase();
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    page.returned({ persisted: false });
    page.fragmentArrived();
    expect(reload).not.toHaveBeenCalled();
    page.returned({ persisted: true });
    location.hash = '#another-secret';
    page.fragmentArrived();
    page.leave();
    page.returned({ persisted: false });
    expect(reload).toHaveBeenCalledTimes(3);
  });
});

describe('read page edge cases', () => {
  prepareReadPage();

  it('moves focus to the title when the opening starts from the reveal form', () => {
    const page = new ReadPage(new Translator('en'));
    ready();
    const area = pageDocument.createElement('div');
    const button = pageDocument.createElement('button');
    area.append(button);
    page.focus.attach('readyArea')(area);
    const title = focusTarget(page, 'readTitle');
    vi.stubGlobal('document', { activeElement: button, body: {}, visibilityState: 'visible' });
    notify({ view: 'busy', step: 'claiming' });
    expect(title).toHaveBeenCalledOnce();
  });

  it('reserves the bar for the opens a resumed lease leaves and returns to the rest after cancelling', async () => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'ready', metadata, resumable: true, deviceFailed: false });
    page.reveal(new SubmitEvent('submit', { cancelable: true }));
    expect(page.reservedOpens).toBe(metadata.opensRemaining);
    notify({ view: 'revealed', text: 'secret', burnable: true, opensRemaining: 1 });
    const rest = focusTarget(page, 'deleteRest');
    await page.askDelete();
    expect(page.reservedOpens).toBe(metadata.opensRemaining);
    await page.cancelDelete();
    expect(rest).toHaveBeenCalledOnce();
  });

  it.each([
    ['ends on a surrogate pair', `${'a'.repeat(1999)}😀tail`, 'tail'],
    ['ends inside plain text', 'a'.repeat(2100), 'a'.repeat(100)],
  ])('scrambles only the head of a long text that %s', async (_, text, rest) => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text, burnable: false, opensRemaining: 0 });
    expect(page.revealRest).toBe(rest);
    await vi.advanceTimersByTimeAsync(1000);
    expect(`${page.revealHead}${page.revealRest}`).toBe(text);
  });

  it('keeps the intent to delete while a deletion was not sent and drops it once it was', async () => {
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    page.passphrase = 'correct horse battery staple';
    await page.askDelete();
    expect(page.deleteIntent).toBe(true);
    reader.burn.mockResolvedValueOnce({ outcome: 'failed' });
    await page.burn();
    expect(page.deleteIntent).toBe(true);
    expect(page.passphrase).toBe('correct horse battery staple');
    reader.burn.mockResolvedValueOnce({ outcome: 'waiting', retryAfter: 60, locked: false });
    await page.burn();
    expect(page.deleteIntent).toBe(false);
  });
});

function focusTarget(page: ReadPage, target: Parameters<typeof page.focus.attach>[0]): ReturnType<typeof vi.fn> {
  const focus = vi.fn<(...arguments_: unknown[]) => void>();
  page.focus.attach(target)(Object.assign(pageDocument.createElement('div'), { focus }));
  return focus;
}

describe('read notices and facts', () => {
  const translator = new Translator('en');

  it('treats a view still in progress as loading', () => {
    expect(noticeOf({ view: 'busy', step: 'claiming' }, undefined)).toBe('loading');
  });

  it.each<BurnOutcome>([{ outcome: 'ended', state: 'expired' }, { outcome: 'unknown' }])(
    'adds no note to a deletion before the reveal that answered $outcome',
    (outcome) => {
      expect(burnText({ outcome, afterReveal: false }, translator)).toBe('');
    },
  );

  it('lists no countdown for an opened message without one', () => {
    expect(openedFacts(translator, { opensRemaining: 0 }).map((fact) => fact.icon)).toEqual(['check', 'eye']);
  });
});
