import { KDF_NONE, KDF_SCRYPT } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Metadata } from '../../shared/api/api.ts';
import { saveFile } from '../../shared/files/save-file.ts';
import { getTranslator, Translator } from '../../shared/language/translator.svelte.ts';
import Read from './Read.svelte';
import type { BurnOutcome, Reader, ReadView } from './reader.ts';
import { createReader } from './reader.ts';

vi.mock('./reader.ts', () => ({ createReader: vi.fn<typeof import('./reader.ts').createReader>() }));
vi.mock('../../shared/files/save-file.ts', () => ({
  revokeSavedFiles: vi.fn<typeof import('../../shared/files/save-file.ts').revokeSavedFiles>(),
  saveFile: vi.fn<typeof import('../../shared/files/save-file.ts').saveFile>(),
}));
vi.mock('../../shared/link/link-opener.ts', () => ({
  getLinkOpener: () => vi.fn<(...arguments_: unknown[]) => void>(),
}));
vi.mock(import('../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
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

function select(start: readonly [Node, number], end: readonly [Node, number]): void {
  const range = document.createRange();
  range.setStart(...start);
  range.setEnd(...end);
  getSelection()?.removeAllRanges();
  getSelection()?.addRange(range);
}
function copy(clipboardData: DataTransfer | null = new DataTransfer()): string | undefined {
  const event = new ClipboardEvent('copy', { clipboardData, cancelable: true, bubbles: true });
  document.dispatchEvent(event);
  return event.defaultPrevented ? clipboardData?.getData('text/plain') : undefined;
}

describe('read interface', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let notify: (view: ReadView) => void;
  const translator = new Translator('en');
  const reader = {
    load: vi.fn<Reader['load']>(),
    reveal: vi.fn<Reader['reveal']>(),
    burn: vi.fn<Reader['burn']>(),
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
  function buttonContaining(label: string): HTMLButtonElement {
    const found = [...target.querySelectorAll('button')].find((element) => element.textContent.includes(label));
    if (!found) {
      throw new Error(`missing button ${label}`);
    }
    return found;
  }
  function passphraseInput(): HTMLInputElement {
    const input = target.querySelector<HTMLInputElement>('input[aria-invalid]');
    if (!input) {
      throw new Error('missing passphrase input');
    }
    return input;
  }
  function message(): HTMLPreElement {
    const element = target.querySelector('pre');
    if (!element) {
      throw new Error('missing message');
    }
    return element;
  }
  function textNode(text: string): Node {
    const walker = document.createTreeWalker(message(), NodeFilter.SHOW_TEXT);
    for (let found = walker.nextNode(); found; found = walker.nextNode()) {
      if (found.textContent === text) {
        return found;
      }
    }
    throw new Error(`missing text ${text}`);
  }
  function show(): void {
    component = mount(Read, { target });
    flushSync();
  }
  async function revealHiddenCharacter(): Promise<void> {
    show();
    view({ view: 'revealed', text: 'hello\u{200B}world', burnable: false, opensRemaining: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    flushSync();
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    reader.load.mockResolvedValue();
    reader.reveal.mockResolvedValue();
    reader.burn.mockResolvedValue({ outcome: 'deleted' });
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
    getSelection()?.removeAllRanges();
    await unmount(component);
    target.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('shows the card once the first answer arrives or the wait runs out, with reveal disabled while loading', () => {
    show();
    expect(target.querySelector('h1')).toBeNull();
    vi.advanceTimersByTime(700);
    flushSync();
    expect(button(translator.translate('read.reveal')).disabled).toBe(true);
    expect(target.textContent).toContain(translator.translate('open.trust.title'));
    ready();
    expect(button(translator.translate('read.reveal')).disabled).toBe(false);
  });

  it.each([
    { reason: 'malformed' },
    { reason: 'long' },
    { reason: 'wrong', attemptsLeft: 2, retryAfter: 0, locked: false },
    { reason: 'wrong', attemptsLeft: 2, retryAfter: 30, locked: false },
  ] as const)('renders passphrase rejection $reason and removes it when typing again', (notice) => {
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT }, notice });
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

  it('tells the reader of a message without a passphrase how long its opening is paused', () => {
    show();
    ready({ metadata, notice: { reason: 'wrong', attemptsLeft: 0, retryAfter: 120, locked: false } });
    const [before = ''] = translator.translate('read.wait', { time: '|' }).split('|');
    expect(target.querySelector('input[type="password"]')).toBeNull();
    expect(before).not.toBe('');
    expect(target.querySelector('[role="alert"]')?.textContent).toContain(before);
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
    { notice: { reason: 'lease_ended' }, key: 'read.lease_ended' },
    { resumable: true, key: 'read.resumable' },
  ] as const)('explains $key before opening', (state) => {
    show();
    ready(state);
    expect(target.textContent).toContain(translator.translate(state.key));
  });

  it('shows progress, reveals hidden characters and confirms copying', async () => {
    show();
    ready();
    view({ view: 'busy', step: 'claiming' });
    expect(button(translator.translate('read.reveal')).disabled).toBe(true);
    view({ view: 'busy', step: 'downloading', fraction: 0.5 });
    view({ view: 'revealed', text: 'hello\u{200B}world', burnable: true, opensRemaining: 1 });
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
    dispatchEvent(new Event('pagehide'));
    flushSync();
    expect(target.querySelector('pre')).toBeNull();
  });

  describe('copying the revealed text by hand', () => {
    it('copies the hidden characters themselves instead of their labels', async () => {
      await revealHiddenCharacter();
      const pre = message();
      const hello = textNode('hello');
      select([pre, 0], [pre, pre.childNodes.length]);
      expect(copy()).toBe('hello\u{200B}world');
      const frame = pre.parentElement;
      if (!frame) {
        throw new Error('missing frame');
      }
      const index = [...frame.childNodes].indexOf(pre);
      select([frame, index], [frame, frame.childNodes.length]);
      expect(copy()).toBe('hello\u{200B}world');
      select([hello, 3], [textNode('U+200B'), 2]);
      expect(copy()).toBe('lo\u{200B}');
    });

    it('leaves a copy alone that holds no hidden character or reaches past the message', async () => {
      await revealHiddenCharacter();
      const pre = message();
      const hello = textNode('hello');
      const title = target.querySelector('h3');
      if (!title) {
        throw new Error('missing title');
      }
      select([hello, 0], [hello, 5]);
      expect(copy()).toBeUndefined();
      select([hello, 3], [textNode('U+200B'), 0]);
      expect(copy()).toBeUndefined();
      select([title, 0], [pre, pre.childNodes.length]);
      expect(copy()).toBeUndefined();
      select([title, 0], [title, 1]);
      expect(copy()).toBeUndefined();
    });

    it('leaves a copy alone without one selection, without clipboard data or while the reader edits text', async () => {
      await revealHiddenCharacter();
      const pre = message();
      getSelection()?.removeAllRanges();
      expect(copy()).toBeUndefined();
      select([pre, 0], [pre, pre.childNodes.length]);
      expect(copy(null)).toBeUndefined();
      for (const editor of [
        document.createElement('input'),
        document.createElement('textarea'),
        Object.assign(document.createElement('div'), { contentEditable: 'true', tabIndex: 0 }),
      ]) {
        document.body.append(editor);
        editor.focus();
        expect(copy()).toBeUndefined();
        editor.remove();
      }
      vi.stubGlobal('getSelection', () => null);
      expect(copy()).toBeUndefined();
    });
  });

  it('requires explicit confirmation to download an attachment, also one named like a PDF', async () => {
    show();
    view({
      view: 'revealed',
      file: { name: 'report.pdf', mime: 'application/pdf', bytes: new Uint8Array([1]) },
      burnable: false,
      opensRemaining: 0,
    });
    const download = [...target.querySelectorAll('button')].find(
      (element) => element.getAttribute('aria-label') === translator.translate('open.download.file'),
    );
    const warning = [...target.querySelectorAll('[role="alertdialog"]')].find((element) =>
      element.textContent.includes(translator.translate('revealed.confirm.title')),
    );
    expect(warning?.hasAttribute('inert')).toBe(true);
    download?.click();
    await tick();
    expect(saveFile).not.toHaveBeenCalled();
    expect(warning?.hasAttribute('inert')).toBe(false);
    warning?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    await tick();
    expect(warning?.hasAttribute('inert')).toBe(true);
    download?.click();
    await tick();
    button(translator.translate('revealed.confirm.cancel')).click();
    await tick();
    expect(saveFile).not.toHaveBeenCalled();
    download?.click();
    await tick();
    const confirm = button(translator.translate('revealed.confirm.download'));
    expect(confirm.disabled).toBe(false);
    expect(confirm.getAttribute('aria-disabled')).toBe('true');
    confirm.click();
    await tick();
    expect(saveFile).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1000);
    expect(confirm.getAttribute('aria-disabled')).toBe('false');
    confirm.click();
    await tick();
    expect(saveFile).toHaveBeenCalledExactlyOnceWith(new Uint8Array([1]), 'report.pdf');
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
    { view: 'timed_out' },
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
  });

  it('keeps a lease notice out of the passphrase field, asks for the passphrase to delete and shows it on request', async () => {
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT }, notice: { reason: 'lease_ended' } });
    expect(passphraseInput().getAttribute('aria-invalid')).toBe('true');
    expect(target.textContent).toContain(translator.translate('read.lease_ended'));
    expect(target.textContent).not.toContain(translator.translate('open.passphrase.empty'));
    button(translator.translate('read.delete')).click();
    await tick();
    expect(target.textContent).toContain(translator.translate('read.delete.passphrase'));
    expect(reader.burn).not.toHaveBeenCalled();
    expect(passphraseInput().type).toBe('password');
    button(translator.translate('open.passphrase.show')).click();
    flushSync();
    expect(passphraseInput().type).toBe('text');
    button(translator.translate('open.passphrase.hide')).click();
    flushSync();
    expect(passphraseInput().type).toBe('password');
  });

  it('deletes the remaining opens from the revealed message only after confirmation and notes the link history', async () => {
    vi.stubGlobal('nylzoFragment', () => '#secret');
    show();
    vi.mocked(createReader).mock.calls[0]?.[0].takeFragment();
    const revealed = { view: 'revealed', text: 'secret', burnable: true, opensRemaining: 1 } as const;
    view(revealed);
    await vi.advanceTimersByTimeAsync(4000);
    flushSync();
    const history = () =>
      [...target.querySelectorAll('span')].find(
        (element) => element.textContent.trim() === translator.translate('revealed.history'),
      );
    expect(history()?.getAttribute('aria-hidden')).toBe('false');
    buttonContaining(translator.translate('read.delete.rest')).click();
    await tick();
    expect(target.textContent).toContain(translator.translate('read.delete.rest.confirm'));
    button(translator.translate('read.delete.no')).click();
    await tick();
    expect(reader.burn).not.toHaveBeenCalled();
    buttonContaining(translator.translate('read.delete.rest')).click();
    await tick();
    reader.burn.mockImplementation(() => {
      notify({ ...revealed, burnable: false, opensRemaining: 0 });
      return Promise.resolve({ outcome: 'deleted' } satisfies BurnOutcome);
    });
    button(translator.translate('read.delete.yes')).click();
    await tick();
    flushSync();
    expect(reader.burn).toHaveBeenCalledExactlyOnceWith('');
    expect(target.textContent).toContain(translator.translate('read.deleted_rest'));
    expect(history()?.getAttribute('aria-hidden')).toBe('true');
    expect(target.querySelector('pre')?.textContent).toBe('secret');
  });

  it('copies nothing when the reader keeps hidden characters off the clipboard, and reports a refused copy', async () => {
    show();
    view({ view: 'revealed', text: 'hello\u{200B}world', burnable: false, opensRemaining: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    flushSync();
    button(translator.translate('open.copy.text')).click();
    await tick();
    button(translator.translate('revealed.hidden.keep')).click();
    await tick();
    expect(target.querySelector('dialog')).toBeNull();
    expect(writeText).not.toHaveBeenCalled();
    view({ view: 'cleared' });
    view({ view: 'revealed', text: 'plain', burnable: false, opensRemaining: 0 });
    await vi.advanceTimersByTimeAsync(4000);
    flushSync();
    expect(target.querySelector('pre > span')?.textContent).toBe('plain');
    writeText.mockRejectedValueOnce(new Error('clipboard denied'));
    button(translator.translate('open.copy.text')).click();
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
    expect(target.querySelector('span[role="status"]')?.textContent).toBe(translator.translate('read.copy_failed'));
  });

  it('explains that a file without an extension has none', () => {
    show();
    view({
      view: 'revealed',
      file: { name: 'README', mime: 'application/octet-stream', bytes: new Uint8Array([1]) },
      burnable: false,
      opensRemaining: 0,
    });
    expect(target.textContent).toContain(translator.translate('revealed.confirm.none'));
  });

  it('forgets the passphrase before unloading, reloads restored pages and pasted links, and ends the time to view', () => {
    const reload = vi.spyOn(location, 'reload').mockImplementation(vi.fn<() => void>());
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT } });
    const input = passphraseInput();
    input.value = 'typed';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    dispatchEvent(new Event('beforeunload'));
    flushSync();
    expect(input.value).toBe('');
    dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }));
    expect(reload).toHaveBeenCalledOnce();
    vi.spyOn(location, 'hash', 'get').mockReturnValue('#pasted');
    dispatchEvent(new Event('hashchange'));
    expect(reload).toHaveBeenCalledTimes(2);
    view({ view: 'revealed', text: 'secret', burnable: false, opensRemaining: 0 });
    vi.setSystemTime(Date.now() + 16 * 60 * 1000);
    document.dispatchEvent(new Event('visibilitychange'));
    flushSync();
    expect(reader.clear).toHaveBeenCalledWith({ view: 'timed_out' });
  });

  it('notes a deletion in progress and marks a failed one', async () => {
    show();
    ready({ metadata: { ...metadata, kdfId: KDF_SCRYPT } });
    const input = passphraseInput();
    input.value = 'correct horse battery staple';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    button(translator.translate('read.delete')).click();
    await tick();
    const gate = Promise.withResolvers<BurnOutcome>();
    reader.burn.mockReturnValueOnce(gate.promise);
    button(translator.translate('read.delete.yes')).click();
    flushSync();
    const note = [...target.querySelectorAll('p:not([role="status"])')].find((element) =>
      element.textContent.includes(translator.translate('busy.deriving')),
    );
    expect(note?.classList.contains('text-rose-200')).toBe(false);
    gate.resolve({ outcome: 'failed' });
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
    expect(note?.textContent).toContain(translator.translate('read.delete.failed'));
    expect(note?.classList.contains('text-rose-200')).toBe(true);
  });
});
