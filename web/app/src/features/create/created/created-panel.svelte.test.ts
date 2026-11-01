// @vitest-environment happy-dom
import { requestKey } from '@nylzo/core';
import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Created } from '../create-message.ts';

import { getTranslator, Translator } from '../../../shared/language/translator.svelte.ts';
import CreatedPanel from './CreatedPanel.svelte';

vi.mock(import('../../../shared/language/translator.svelte.ts'), (original) =>
  // eslint-disable-next-line unicorn/prefer-await -- Svelte instruments awaits before hoisted imports initialize.
  original().then((actual) => ({ ...actual, getTranslator: vi.fn() })),
);

describe('created message panel', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  let created: Created;
  const translator = new Translator('en');
  const another = vi.fn();
  const writeText = vi.fn<(text: string) => Promise<void>>();
  const share = vi.fn<() => Promise<void>>();
  const canShare = vi.fn<() => boolean>();

  function show(passphrase = false, opensMax = 1): void {
    component = mount(CreatedPanel, { target, props: { created, opensMax, passphrase, onanother: another } });
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
    vi.stubGlobal('navigator', { clipboard: { writeText }, share, canShare });
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
    show(true, 3);
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
    expect(target.textContent).toContain(translator.translate('created.sub.passphrase'));
    expect(target.textContent).toContain(translator.translate('compose.opens.other', { count: 3 }));
    button('created.another').click();
    expect(another).toHaveBeenCalledOnce();
  });

  it('copies the complete secret link, clears the clipboard and expires the success indicator', async () => {
    show();
    button('created.copy').click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith(created.link);
    expect(target.textContent).toContain(translator.translate('created.copied'));
    button('clipboard.clear').click();
    await tick();
    expect(writeText).toHaveBeenLastCalledWith('');
    expect(button('clipboard.clear').disabled).toBe(true);
    await vi.advanceTimersByTimeAsync(2500);
    expect(target.querySelector('[role="status"]')?.textContent).toBe('');
  });

  it('selects the link for manual copy if the clipboard API refuses', async () => {
    writeText.mockRejectedValue(new Error('permission denied'));
    show();
    button('created.copy').click();
    await tick();
    expect(document.activeElement).toBe(target.querySelector('input'));
    expect(target.querySelector('[role="status"]')?.textContent).toBe('');
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

  it('hides native sharing when the browser cannot share a link', () => {
    canShare.mockReturnValue(false);
    show();
    const label = translator.translate('created.share');
    expect(target.querySelector(`button[aria-label="${CSS.escape(label)}"]`)).toBeNull();
  });
});
