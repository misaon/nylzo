// @vitest-environment happy-dom
import { requestKey } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../../shared/language/translator.svelte.ts';
import type { Remembered } from '../composer/composer.svelte.ts';
import type { Created } from '../create-message.ts';
import CreatedPanel from './CreatedPanel.svelte';

vi.mock(import('../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

describe('created message panel', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let created: Created;
  const translator = new Translator('en');
  const another = vi.fn<() => void>();
  const remember = vi.fn<(label: string, keep: boolean) => void>();
  const writeText = vi.fn<(text: string) => Promise<void>>();
  const share = vi.fn<() => Promise<void>>();
  const canShare = vi.fn<() => boolean>();
  const coarsePointer = vi.fn<() => boolean>();

  function show({
    passphrase = '',
    opensMax = 1,
    canKeep = true,
    canTrackInTab = true,
    remembered = 'off',
  }: {
    readonly passphrase?: string;
    readonly opensMax?: number;
    readonly canKeep?: boolean;
    readonly canTrackInTab?: boolean;
    readonly remembered?: Remembered;
  } = {}): void {
    component = mount(CreatedPanel, {
      target,
      props: {
        created,
        opensMax,
        passphrase,
        canKeep,
        canTrackInTab,
        remembered,
        onremember: remember,
        onanother: another,
      },
    });
    flushSync();
  }
  function button(key: Parameters<typeof translator.translate>[0]): HTMLButtonElement {
    const label = translator.translate(key);
    const found = [...target.querySelectorAll('button')].find(
      (element) => element.getAttribute('aria-label') === label || element.textContent.trim() === label,
    );
    if (!found) {
      throw new Error(`missing button ${label}`);
    }
    return found;
  }
  function status(): string {
    return target.querySelector('[role="status"]')?.textContent.trim() ?? '';
  }

  beforeEach(async () => {
    vi.useFakeTimers();
    vi.resetAllMocks();
    vi.mocked(getTranslator).mockReturnValue(translator);
    created = {
      id: 'AAAAAAAAAAAAAAAAAAAAAA',
      link: 'https://nylzo.test/r#2.0.AAAAAAAAAAAAAAAAAAAAAA.key.share',
      expiresAt: '2026-11-01T15:00:00Z',
      manageKey: await requestKey(new Uint8Array(32).fill(7)),
    };
    writeText.mockResolvedValue();
    share.mockResolvedValue();
    canShare.mockReturnValue(true);
    coarsePointer.mockReturnValue(true);
    vi.stubGlobal('navigator', { clipboard: { writeText }, share, canShare });
    vi.stubGlobal('matchMedia', () => ({ matches: coarsePointer() }));
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('focuses the success title, preserves the exact link and offers another message', async () => {
    show({ opensMax: 3 });
    await vi.advanceTimersByTimeAsync(30);
    expect(document.activeElement).toBe(target.querySelector('h2'));
    const input = target.querySelector('input');
    expect(input?.value).toBe(created.link);
    expect(input?.readOnly).toBe(true);
    input?.focus();
    flushSync();
    expect(input?.selectionStart).toBe(0);
    expect(input?.selectionEnd).toBe(created.link.length);
    input?.blur();
    flushSync();
    expect(target.textContent).toContain(translator.plural('compose.opens', 3));
    button('created.another').click();
    expect(another).toHaveBeenCalledOnce();
  });

  it('copies the complete secret link, clears the clipboard and expires the success indicator', async () => {
    show();
    button('created.copy').click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith(created.link);
    expect(status()).toContain(translator.translate('clipboard.copied'));
    vi.advanceTimersToNextFrame();
    const clear = button('clipboard.clear');
    clear.focus();
    clear.click();
    await tick();
    expect(writeText).toHaveBeenLastCalledWith('');
    expect(target.textContent).not.toContain(translator.translate('clipboard.clear'));
    expect(status()).toContain(translator.translate('clipboard.cleared'));
    await vi.waitFor(() => expect(document.activeElement?.getAttribute('role')).toBe('status'));
  });

  it('selects the link for manual copy if the clipboard API refuses', async () => {
    writeText.mockRejectedValue(new Error('permission denied'));
    show();
    button('created.copy').click();
    await tick();
    expect(document.activeElement).toBe(target.querySelector('input'));
    expect(status()).toBe(translator.translate('created.copy_failed'));
  });

  it('keeps clearing available if the clipboard refuses to clear', async () => {
    show();
    button('created.copy').click();
    await tick();
    writeText.mockRejectedValue(new Error('denied'));
    button('clipboard.clear').click();
    await tick();
    expect(button('clipboard.clear').disabled).toBe(false);
  });

  it('masks the passphrase until asked and copies it exactly', async () => {
    const passphrase = 'sova-lampa-reka-kamen';
    show({ passphrase });
    expect(target.textContent).not.toContain(passphrase);
    button('created.passphrase.show').click();
    flushSync();
    expect(target.textContent).toContain(passphrase);
    button('created.passphrase.copy').click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith(passphrase);
  });

  it('saves to Sent only when asked, with the typed label, in this tab unless the sender keeps it', () => {
    show();
    const label = target.querySelector<HTMLInputElement>(
      `input[placeholder="${CSS.escape(translator.translate('created.track.placeholder'))}"]`,
    );
    const keep = target.querySelector<HTMLButtonElement>('button[role="switch"]');
    const save = () => target.querySelector<HTMLButtonElement>('form button[type="submit"]')?.click();
    if (!label || !keep) {
      throw new Error('missing label field or keep switch');
    }
    expect(remember).not.toHaveBeenCalled();
    const note = () => target.querySelector(`#${CSS.escape(keep.getAttribute('aria-describedby') ?? '')}`);
    expect(keep.getAttribute('aria-checked')).toBe('false');
    expect(note()?.textContent.trim()).toBe(translator.translate('created.track.note'));
    expect(note()?.getAttribute('aria-hidden')).toBe('false');
    label.value = 'Password for Peter';
    label.dispatchEvent(new Event('input', { bubbles: true }));
    save();
    expect(remember).toHaveBeenLastCalledWith('Password for Peter', false);
    keep.click();
    flushSync();
    expect(keep.getAttribute('aria-checked')).toBe('true');
    expect(note()?.textContent.trim()).toBe(translator.translate('created.track.note.kept'));
    expect(note()?.getAttribute('aria-hidden')).toBe('false');
    save();
    expect(remember).toHaveBeenLastCalledWith('Password for Peter', true);
  });

  it.each([
    { name: 'the page cannot keep it in the browser', props: { canKeep: false }, keep: false },
    { name: 'the browser lacks the Navigation API', props: { canTrackInTab: false }, keep: true },
    { name: 'a browser save failed', props: { remembered: 'failed' as Remembered }, keep: false },
  ])('offers one place to track when $name', ({ props, keep }) => {
    show(props);
    const toggle = target.querySelector<HTMLButtonElement>('button[role="switch"]');
    expect(toggle === null || toggle.disabled).toBe(true);
    target.querySelector<HTMLButtonElement>('form button[type="submit"]')?.click();
    expect(remember).toHaveBeenCalledExactlyOnceWith('', keep);
  });

  it.each([false, true])('handles native sharing and cancellation: cancelled=%s', async (cancelled) => {
    if (cancelled) {
      share.mockRejectedValue(new DOMException('cancelled', 'AbortError'));
    }
    show();
    button('created.share').click();
    await tick();
    expect(share).toHaveBeenCalledExactlyOnceWith({ url: created.link });
    expect(target.querySelector('input')?.value).toBe(created.link);
  });

  it.each([
    ['cannot share a link', () => canShare.mockReturnValue(false)],
    ['has a fine pointer', () => coarsePointer.mockReturnValue(false)],
  ])('hides native sharing when the browser %s', (_reason, arrange) => {
    arrange();
    show();
    expect([...target.querySelectorAll('button')].map((element) => element.textContent.trim())).not.toContain(
      translator.translate('created.share'),
    );
  });
});
