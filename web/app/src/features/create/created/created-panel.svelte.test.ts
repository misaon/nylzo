// @vitest-environment happy-dom
import { requestKey } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../../shared/language/translator.svelte.ts';
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
  const remember = vi.fn<(label: string) => void>();
  const writeText = vi.fn<(text: string) => Promise<void>>();
  const share = vi.fn<() => Promise<void>>();
  const canShare = vi.fn<() => boolean>();
  const coarsePointer = vi.fn<() => boolean>();

  function show({ passphrase = '', opensMax = 1, canRemember = true } = {}): void {
    component = mount(CreatedPanel, {
      target,
      props: {
        created,
        opensMax,
        passphrase,
        canRemember,
        remembered: 'off',
        onremember: remember,
        onanother: another,
        leaving: false,
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
    expect(target.textContent).toContain(translator.translate('compose.opens.other', { count: 3 }));
    button('created.another').click();
    expect(another).toHaveBeenCalledOnce();
  });

  it('copies the complete secret link, clears the clipboard and expires the success indicator', async () => {
    show();
    button('created.copy').click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith(created.link);
    expect(status()).toContain(translator.translate('clipboard.copied'));
    button('clipboard.clear').click();
    await tick();
    expect(writeText).toHaveBeenLastCalledWith('');
    expect(button('clipboard.clear').disabled).toBe(true);
    expect(status()).toContain(translator.translate('clipboard.cleared'));
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

  it('saves to Sent only when asked, with the typed label, and never in the local client', async () => {
    show();
    const label = target.querySelector<HTMLInputElement>(
      `input[placeholder="${CSS.escape(translator.translate('created.track.placeholder'))}"]`,
    );
    if (!label) {
      throw new Error('missing label field');
    }
    expect(remember).not.toHaveBeenCalled();
    label.value = 'Password for Peter';
    label.dispatchEvent(new Event('input', { bubbles: true }));
    target.querySelector<HTMLButtonElement>('form button[type="submit"]')?.click();
    expect(remember).toHaveBeenCalledExactlyOnceWith('Password for Peter');
    await unmount(component);
    show({ canRemember: false });
    expect(target.textContent).not.toContain(translator.translate('created.track.title'));
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
