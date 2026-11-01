// @vitest-environment happy-dom
import { KDF_NONE, KDF_SCRYPT } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Metadata } from '../../shared/api/api.ts';
import type { BurnOutcome, Reader, ReadView } from './reader.ts';

import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import Read from './Read.svelte';
import { createReader } from './reader.ts';
import { saveFile } from './save-file.ts';

vi.mock('./reader.ts', () => ({ createReader: vi.fn() }));
vi.mock('./save-file.ts', () => ({ saveFile: vi.fn() }));
vi.mock('../../shared/link/link-opener.ts', () => ({ getLinkOpener: () => vi.fn() }));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Svelte instruments awaits before hoisted imports initialize.
  original().then((actual) => ({ ...actual, getTranslator: vi.fn() })),
);

const metadata: Metadata = {
  state: 'available',
  opensMax: 3,
  opensRemaining: 2,
  ttlSeconds: 3600,
  kdfId: KDF_NONE,
  size: 100,
  expiresAt: '2026-11-01T15:00:00Z',
};

describe('read interface', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let notify: (view: ReadView) => void;
  const translator = new Translator('en');
  const reader = {
    load: vi.fn<Reader['load']>(),
    reveal: vi.fn<Reader['reveal']>(),
    burn: vi.fn<Reader['burn']>(),
    report: vi.fn<Reader['report']>(),
    clear: vi.fn<Reader['clear']>(),
  };
  const writeText = vi.fn<(text: string) => Promise<void>>();

  function view(next: ReadView): void {
    notify(next);
    flushSync();
  }
  function ready(overrides: Partial<Extract<ReadView, { view: 'ready' }>> = {}): void {
    view({ view: 'ready', metadata, resumable: false, deviceFailed: false, ...overrides });
  }
  function button(label: string): HTMLButtonElement {
    const found = [...target.querySelectorAll('button')].find(
      (element) => element.getAttribute('aria-label') === label || element.textContent.trim() === label,
    );
    if (!found) {
      throw new Error(`missing button ${label}`);
    }
    return found;
  }
  function show(): void {
    component = mount(Read, { target });
    flushSync();
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    reader.load.mockResolvedValue();
    reader.reveal.mockResolvedValue();
    reader.burn.mockResolvedValue({ outcome: 'deleted' });
    reader.report.mockResolvedValue({ outcome: 'accepted' });
    reader.clear.mockImplementation(() => notify({ view: 'cleared' }));
    vi.mocked(createReader).mockImplementation((_dependencies, onView) => {
      notify = onView;
      return reader;
    });
    writeText.mockResolvedValue();
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('keeps reveal disabled while loading and renders the privacy guarantees', () => {
    show();
    expect(button(translator.translate('read.reveal')).disabled).toBe(true);
    expect(target.textContent).toContain(translator.translate('open.trust.title'));
    ready();
    expect(button(translator.translate('read.reveal')).disabled).toBe(false);
  });

  it.each([
    { reason: 'malformed' },
    { reason: 'wrong_or_changed' },
    { reason: 'wrong', attemptsLeft: 2, retryAfter: 0, locked: false },
    { reason: 'wrong', attemptsLeft: 2, retryAfter: 30, locked: false },
  ] as const)('renders passphrase rejection $reason and removes it when typing again', (rejected) => {
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT }, rejected });
    const input = target.querySelector<HTMLInputElement>('input[type="password"]');
    expect(input?.getAttribute('aria-invalid')).toBe('true');
    if (!input) {
      throw new Error('missing passphrase input');
    }
    input.value = 'try again';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    flushSync();
    expect(input.getAttribute('aria-invalid')).toBe('false');
  });

  it('validates an empty passphrase, toggles its visibility and sends it once', async () => {
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT } });
    button(translator.translate('read.reveal')).click();
    await tick();
    expect(target.textContent).toContain(translator.translate('open.passphrase.empty'));
    expect(reader.reveal).not.toHaveBeenCalled();
    const input =
      target.querySelector<HTMLInputElement>('input[autocomplete="new-password"]') ??
      target.querySelector<HTMLInputElement>('input[type="password"]');
    if (!input) {
      throw new Error('missing passphrase input');
    }
    input.value = 'synthetic correct horse battery staple';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    button(translator.translate('read.reveal')).click();
    await tick();
    expect(reader.reveal).toHaveBeenCalledExactlyOnceWith('synthetic correct horse battery staple');
    expect(input.value).toBe('');
  });

  it.each([
    { deviceFailed: true, key: 'read.device' },
    { leaseEnded: true, key: 'read.lease_ended' },
    { resumable: true, key: 'read.resumable' },
  ] as const)('explains $key before opening', (state) => {
    show();
    ready(state);
    expect(target.textContent).toContain(translator.translate(state.key));
  });

  it('shows progress, reveals hidden characters, confirms copying, and clears the clipboard', async () => {
    show();
    ready();
    view({ view: 'busy', step: 'claiming' });
    expect(button(translator.translate('read.reveal')).disabled).toBe(true);
    view({ view: 'busy', step: 'downloading', fraction: 0.5 });
    view({ view: 'revealed', text: 'hello\u{200B}world', burnable: true });
    await vi.advanceTimersByTimeAsync(4000);
    flushSync();
    expect(target.querySelector('mark')).not.toBeNull();
    expect(target.textContent).toContain(translator.translate('revealed.hidden', { count: 1 }));
    button(translator.translate('open.copy.text')).click();
    await tick();
    expect(target.querySelector('dialog')?.open).toBe(true);
    expect(writeText).not.toHaveBeenCalled();
    button(translator.translate('revealed.hidden.copy')).click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith('hello\u{200B}world');
    button(translator.translate('clipboard.clear')).click();
    await tick();
    expect(writeText).toHaveBeenLastCalledWith('');
    expect(target.textContent).toContain(translator.translate('clipboard.cleared'));
    dispatchEvent(new Event('pagehide'));
    flushSync();
    expect(target.querySelector('pre')).toBeNull();
  });

  it('requires explicit confirmation to download a potentially executable attachment', async () => {
    show();
    view({
      view: 'revealed',
      file: { name: 'script.sh', mime: 'text/plain', bytes: new Uint8Array([1]) },
      burnable: false,
    });
    const download = [...target.querySelectorAll('button')].find((element) =>
      element.textContent.includes(translator.translate('open.download.suffix')),
    );
    download?.click();
    await tick();
    expect(saveFile).not.toHaveBeenCalled();
    expect(target.textContent).toContain(translator.translate('revealed.confirm.title'));
    button(translator.translate('revealed.confirm.cancel')).click();
    await tick();
    expect(saveFile).not.toHaveBeenCalled();
    download?.click();
    await tick();
    button(translator.translate('revealed.confirm.download')).click();
    await tick();
    expect(saveFile).toHaveBeenCalledExactlyOnceWith(new Uint8Array([1]), 'script.sh');
  });

  it('asks before deleting, supports Escape, then reports a successful deletion', async () => {
    show();
    ready();
    button(translator.translate('read.delete')).click();
    await tick();
    const dialog = target.querySelector('[role="alertdialog"]');
    expect(dialog?.hasAttribute('inert')).toBe(false);
    dialog?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await tick();
    expect(reader.burn).not.toHaveBeenCalled();
    button(translator.translate('read.delete')).click();
    await tick();
    reader.burn.mockImplementation(() => {
      notify({ view: 'ended', state: 'deleted' });
      return Promise.resolve({ outcome: 'deleted' } satisfies BurnOutcome);
    });
    button(translator.translate('read.delete.yes')).click();
    await tick();
    expect(reader.burn).toHaveBeenCalledOnce();
    expect(target.textContent).toContain(translator.translate('open.notice.burned'));
  });

  it.each<ReadView>([
    { view: 'failed' },
    { view: 'retry_later', retryAfter: 30 },
    { view: 'invalid' },
    { view: 'unknown' },
    { view: 'cleared' },
    { view: 'ended', state: 'expired' },
  ])('renders $view notices and retries only transient failures', (state) => {
    show();
    view(state);
    const retry = [...target.querySelectorAll('button')].find(
      (element) => element.textContent.trim() === translator.translate('action.retry'),
    );
    const retryable = state.view === 'failed' || state.view === 'retry_later';
    expect(retry !== undefined).toBe(retryable);
    retry?.click();
    expect(reader.load).toHaveBeenCalledTimes(retryable ? 2 : 1);
    expect(target.querySelector('input[type="text"]') !== null).toBe(state.view === 'invalid');
  });
});
