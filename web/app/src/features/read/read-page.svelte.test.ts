import { KDF_NONE, KDF_SCRYPT } from '@nylzo/core';
import { flushSync, onDestroy } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Metadata } from '../../shared/api/api.ts';
import type { BurnOutcome, ReaderDependencies, ReadView } from './reader.ts';

import { Translator } from '../../shared/language/translator.svelte.ts';
import { takeFragment } from '../../shared/link/link-handoff.ts';
import { ReadPage } from './read-page.svelte.ts';
import { createReader } from './reader.ts';
import { saveFile } from './save-file.ts';

// Avoid an await in this hoisted factory: Svelte instruments awaits with a runtime import.
vi.mock(import('svelte'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Await instrumentation cannot run before imports initialize.
  original().then((actual) => ({ ...actual, onDestroy: vi.fn() })),
);
vi.mock('./reader.ts', () => ({ createReader: vi.fn() }));
vi.mock('./save-file.ts', () => ({ saveFile: vi.fn() }));
vi.mock('../../shared/link/link-handoff.ts', () => ({ takeFragment: vi.fn() }));

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
function reducedMotion(matches: boolean): void {
  const query = matchMedia('(prefers-reduced-motion: reduce)') as MediaQueryList & { matches: boolean };
  query.matches = matches;
  query.dispatchEvent(new Event('change'));
  flushSync();
}

describe('read page interactions', () => {
  const reader = {
    load: vi.fn<() => Promise<void>>(),
    reveal: vi.fn<(passphrase?: string) => Promise<void>>(),
    burn: vi.fn<() => Promise<BurnOutcome>>(),
    report: vi.fn<() => Promise<{ outcome: 'accepted' }>>(),
    clear: vi.fn<() => void>(),
  };
  let notify: (view: ReadView) => void;
  let dependencies: ReaderDependencies;
  let destroy: (() => void)[];

  function ready(overrides: Partial<Metadata> = {}): void {
    notify({ view: 'ready', metadata: { ...metadata, ...overrides }, resumable: false, deviceFailed: false });
  }

  function focusTarget(page: ReadPage, target: Parameters<typeof page.focus.attach>[0]): ReturnType<typeof vi.fn> {
    const focus = vi.fn();
    page.focus.attach(target)({ focus } as unknown as HTMLElement);
    return focus;
  }

  const writeText = vi.fn<(text: string) => Promise<void>>();
  const reload = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    destroy = [];
    vi.mocked(onDestroy).mockImplementation((callback) => {
      destroy.push(callback);
    });
    vi.mocked(takeFragment).mockReturnValue({ fragment: '#secret', pasted: false });
    reader.load.mockResolvedValue();
    reader.reveal.mockResolvedValue();
    reader.burn.mockResolvedValue({ outcome: 'deleted' });
    reader.report.mockResolvedValue({ outcome: 'accepted' });
    reader.clear.mockImplementation(() => notify({ view: 'cleared' }));
    vi.mocked(createReader).mockImplementation((options, onView) => {
      dependencies = options;
      notify = onView;
      return reader;
    });
    vi.stubGlobal('document', { activeElement: null, body: {}, visibilityState: 'visible' });
    vi.stubGlobal('location', { pathname: '/m/example', hash: '', reload });
    vi.stubGlobal('sessionStorage', { getItem: vi.fn(), setItem: vi.fn(), removeItem: vi.fn() });
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

  it('loads once and records whether the secret link was already in browser history', () => {
    const page = new ReadPage(new Translator('en'));
    expect(reader.load).toHaveBeenCalledExactlyOnceWith();
    expect(page.status).toBe(new Translator('en').translate('read.loading'));
    expect(dependencies.takeFragment()).toBe('#secret');
    expect(takeFragment).toHaveBeenCalledWith('/m/example');
    expect(page.linkInHistory).toBe(true);
    vi.mocked(takeFragment).mockReturnValue({ fragment: '#pasted', pasted: true });
    dependencies.takeFragment();
    expect(page.linkInHistory).toBe(false);
    page.retry();
    expect(reader.load).toHaveBeenCalledTimes(2);
  });

  it('requires a passphrase before revealing and clears it before handing it to the reader', async () => {
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    const focus = focusTarget(page, 'passphrase');
    const event = new Event('submit', { cancelable: true }) as SubmitEvent;
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
    notify({ view: 'revealed', text: 'Hello 🌍\nworld', burnable: true });
    const displayed = vi.fn();
    const shown = (async () => {
      await dependencies.shown?.();
      displayed();
    })();
    vi.advanceTimersByTime(2500);
    expect(page.revealed).toBeUndefined();
    expect(displayed).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2000);
    await shown;
    expect(page.phase).toBe('revealed');
    expect(page.revealText).toBe('Hello 🌍\nworld');
    expect(focused).toHaveBeenCalledOnce();
    expect(page.reportRound).toBe(1);
    expect(page.reportable).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('uses a shorter timeline and skips scrambling for reduced motion', async () => {
    reducedMotion(true);
    const page = new ReadPage(new Translator('en'));
    ready({ kdfId: KDF_SCRYPT });
    expect(page.steps).toHaveLength(4);
    notify({ view: 'busy', step: 'deriving' });
    notify({ view: 'revealed', text: 'Private text', burnable: false });
    await vi.advanceTimersByTimeAsync(1100);
    expect(page.revealText).toBe('Private text');
    expect(page.phase).toBe('revealed');
    expect(page.reportable).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
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
      rejected: { reason: 'wrong', attemptsLeft: 0, retryAfter: 30, locked: true },
    });
    await Promise.resolve();
    expect(focus).toHaveBeenCalledOnce();
    expect(page.phase).toBe('notice');
    expect(page.notice).toBe('locked');
    page.reveal(new Event('submit') as SubmitEvent);
    expect(reader.reveal).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('downloads safe attachments directly and requires confirmation for executable files', async () => {
    const page = new ReadPage(new Translator('en'));
    const click = { detail: 1 } as MouseEvent;
    const doubleClick = { detail: 2 } as MouseEvent;
    const unsafe = { ...file, name: 'script.sh' };
    await page.askDownload(doubleClick, file);
    expect(saveFile).not.toHaveBeenCalled();
    await page.askDownload(click, file);
    expect(saveFile).toHaveBeenCalledExactlyOnceWith(file.bytes, file.name);
    expect(page.downloaded.shown).toBe(true);
    const keep = focusTarget(page, 'keepFile');
    await page.askDownload(click, unsafe);
    expect(page.confirmingDownload).toBe(true);
    expect(keep).toHaveBeenCalledOnce();
    await page.confirmDownload(doubleClick, unsafe);
    expect(saveFile).toHaveBeenCalledTimes(1);
    await page.confirmDownload(click, unsafe);
    expect(saveFile).toHaveBeenLastCalledWith(unsafe.bytes, unsafe.name);
    expect(page.confirmingDownload).toBe(false);
    await page.confirmDownload(click, unsafe);
    expect(saveFile).toHaveBeenCalledTimes(2);
    await page.askDownload(click, unsafe);
    await page.cancelDownload(doubleClick);
    expect(page.confirmingDownload).toBe(true);
    await page.cancelDownload(click);
    expect(page.confirmingDownload).toBe(false);
    vi.advanceTimersByTime(2400);
    expect(page.downloaded.shown).toBe(false);
  });

  it('warns before copying hidden characters and copies the original text only after confirmation', async () => {
    reducedMotion(true);
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text: 'hello\u{200B}world', burnable: false });
    await page.askCopy();
    expect(page.confirmingCopy).toBe(true);
    expect(writeText).not.toHaveBeenCalled();
    page.cancelCopy();
    expect(page.confirmingCopy).toBe(false);
    await page.askCopy();
    await page.copyText();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('hello\u{200B}world');
    expect(page.clipboard).toBe('copied');
    expect(page.copied.shown).toBe(true);
    await page.clearClipboard();
    expect(writeText).toHaveBeenLastCalledWith('');
    expect(page.clipboard).toBe('cleared');
  });

  it('handles clipboard refusal without a false success indication', async () => {
    const page = new ReadPage(new Translator('en'));
    await page.copyText();
    expect(writeText).not.toHaveBeenCalled();
    notify({ view: 'revealed', text: 'hello', burnable: false });
    await page.askCopy();
    expect(page.copied.shown).toBe(true);
    vi.mocked(writeText).mockRejectedValue(new Error('clipboard denied'));
    await page.copyText();
    expect(page.copied.shown).toBe(false);
    await page.clearClipboard();
    expect(page.clipboard).toBe('copied');
  });

  it.each<BurnOutcome>([
    { outcome: 'deleted' },
    { outcome: 'ended', state: 'expired' },
    { outcome: 'failed' },
    { outcome: 'retry_later', retryAfter: 30 },
    { outcome: 'unknown' },
    { outcome: 'ignored' },
  ])('preserves the content and reports the result of deleting remaining opens: $outcome', async (outcome) => {
    const page = new ReadPage(new Translator('en'));
    notify({ view: 'revealed', text: 'keep on screen', burnable: true });
    const gate = Promise.withResolvers<BurnOutcome>();
    reader.burn.mockReturnValueOnce(gate.promise);
    const burning = page.burn();
    expect(page.deleting).toBe(true);
    gate.resolve(outcome);
    await burning;
    expect(page.deleting).toBe(false);
    expect(page.revealed?.text).toBe('keep on screen');
    expect(page.restDeleted).toBe(outcome.outcome === 'deleted' || outcome.outcome === 'ended');
    expect(page.burnFailed).toBe(['failed', 'retry_later', 'unknown'].includes(outcome.outcome));
    expect(page.burnNote === '').toBe(outcome.outcome === 'ignored');
    expect(page.burned).toEqual({ outcome, afterReveal: true });
  });

  it('moves focus safely through deletion confirmation and restores actions when cancelled', async () => {
    const page = new ReadPage(new Translator('en'));
    ready();
    const keep = focusTarget(page, 'keepMessage');
    const remove = focusTarget(page, 'deleteMessage');
    page.reporting = true;
    await page.askDelete();
    expect(page.reporting).toBe(false);
    expect(page.actions).toBe('off');
    expect(keep).toHaveBeenCalledOnce();
    const other = new Event('keydown');
    page.onConfirmKeydown(other as KeyboardEvent);
    expect(page.askingDelete).toBe(true);
    const escape = { key: 'Escape', stopPropagation: vi.fn() };
    page.onConfirmKeydown(escape as unknown as KeyboardEvent);
    await Promise.resolve();
    expect(escape.stopPropagation).toHaveBeenCalledOnce();
    expect(page.askingDelete).toBe(false);
    expect(page.actions).toBe('on');
    expect(remove).toHaveBeenCalledOnce();
  });

  it('submits reports, announces their status and keeps clicks inside the report menu open', async () => {
    const translator = new Translator('en');
    const page = new ReadPage(translator);
    ready();
    const popover = focusTarget(page, 'reportPopover');
    page.toggleReport();
    const menu = Object.assign(new Element(), { closest: () => ({}) });
    page.onWindowClick({ target: menu } as unknown as MouseEvent);
    expect(page.reporting).toBe(true);
    const report = {
      link: 'https://nylzo.test/m/example#secret',
      reason: 'synthetic report',
      contact: '',
      goodFaith: true,
    };
    await expect(page.report(report)).resolves.toEqual({ outcome: 'accepted' });
    expect(reader.report).toHaveBeenCalledExactlyOnceWith({
      reason: report.reason,
      contact: report.contact,
      goodFaith: report.goodFaith,
    });
    await page.reportFinished('accepted');
    expect(popover).toHaveBeenCalledOnce();
    expect(page.status).toBe(translator.translate('open.report.sent'));
    await page.reportFinished('limit');
    expect(page.status).toBe(translator.translate('open.report.limit'));
    page.onWindowClick({ target: null } as MouseEvent);
    expect(page.reporting).toBe(false);
    await page.closeReport();
    expect(page.reporting).toBe(false);
  });

  it('shows metadata and end-state chips in the selected language', () => {
    const translator = new Translator('cs');
    const page = new ReadPage(translator);
    expect(page.readyChips).toEqual([]);
    ready({ kdfId: KDF_SCRYPT });
    expect(page.readyChips.map((chip) => chip.icon)).toEqual(['clock', 'eye', 'key']);
    expect(page.opensLeft(1)).toBe(translator.translate('open.chip.opens.one', { count: 1 }));
    expect(page.opensLeft(2)).toBe(translator.translate('open.chip.opens.few', { count: 2 }));
    expect(page.opensLeft(5)).toBe(translator.translate('open.chip.opens.other', { count: 5 }));
    notify({ view: 'ended', state: 'opened', firstOpenedAt: metadata.expiresAt });
    expect(page.afterChips[0]?.icon).toBe('eye');
    notify({ view: 'ended', state: 'expired', endedAt: metadata.expiresAt });
    expect(page.afterChips[0]?.icon).toBe('clock');
    notify({ view: 'ended', state: 'deleted' });
    page.burned = { outcome: { outcome: 'deleted' }, afterReveal: false };
    expect(page.notice).toBe('burned');
    expect(page.afterChips[0]?.text).toBe(translator.translate('open.chip.burned'));
  });

  it('wipes page state on leave, settles an outstanding display wait, and stops all animation', async () => {
    const page = new ReadPage(new Translator('en'));
    ready();
    notify({ view: 'busy', step: 'claiming' });
    notify({ view: 'revealed', text: 'must not reappear', burnable: true });
    const displayed = dependencies.shown?.();
    page.passphrase = 'secret';
    page.showPassphrase = true;
    page.confirmingCopy = true;
    page.confirmingDownload = true;
    page.askingDelete = true;
    page.reporting = true;
    page.leave();
    await displayed;
    vi.advanceTimersByTime(10_000);
    expect(reader.clear).toHaveBeenCalledOnce();
    expect(page.shown).toEqual({ view: 'cleared' });
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    expect(page.confirmingCopy).toBe(false);
    expect(page.confirmingDownload).toBe(false);
    expect(page.askingDelete).toBe(false);
    expect(page.reporting).toBe(false);
    expect(page.revealText).toBeUndefined();
    expect(page.marked).toBeUndefined();
    expect(page.opening).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('forgets a visible passphrase and reloads restored documents or newly pasted fragments', () => {
    const page = new ReadPage(new Translator('en'));
    page.passphrase = 'secret';
    page.showPassphrase = true;
    page.forgetPassphrase();
    expect(page.passphrase).toBe('');
    expect(page.showPassphrase).toBe(false);
    page.returned({ persisted: false } as PageTransitionEvent);
    page.fragmentArrived();
    expect(reload).not.toHaveBeenCalled();
    page.returned({ persisted: true } as PageTransitionEvent);
    location.hash = '#another-secret';
    page.fragmentArrived();
    page.leave();
    page.returned({ persisted: false } as PageTransitionEvent);
    expect(reload).toHaveBeenCalledTimes(3);
  });
});
