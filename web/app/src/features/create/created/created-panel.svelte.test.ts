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
  const remember = vi.fn<(label: string) => void>();
  const trackingLeft = vi.fn<() => void>();
  const writeText = vi.fn<(text: string) => Promise<void>>();
  const share = vi.fn<() => Promise<void>>();
  const canShare = vi.fn<() => boolean>();
  const coarsePointer = vi.fn<() => boolean>();

  function show({
    passphrase = '',
    opensMax = 1,
    trackable = true,
    keeps = true,
    remembered = 'off',
  }: {
    readonly passphrase?: string;
    readonly opensMax?: number;
    readonly trackable?: boolean;
    readonly keeps?: boolean;
    readonly remembered?: Remembered;
  } = {}): void {
    component = mount(CreatedPanel, {
      target,
      props: {
        created,
        opensMax,
        passphrase,
        trackable,
        keeps,
        remembered,
        onremember: remember,
        ontrackingleft: trackingLeft,
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

  it('copies the complete secret link and announces it', async () => {
    show();
    button('created.copy').click();
    await tick();
    expect(writeText).toHaveBeenCalledExactlyOnceWith(created.link);
    expect(status()).toBe(translator.translate('action.copied'));
  });

  it('selects the link for manual copy and says so if the clipboard API refuses', async () => {
    writeText.mockRejectedValue(new Error('permission denied'));
    show();
    button('created.copy').click();
    await tick();
    expect(document.activeElement).toBe(target.querySelector('input'));
    expect(target.querySelector('[role="alert"]')?.textContent.trim()).toBe(
      translator.translate('created.copy_failed'),
    );
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

  it('shows the passphrase when it cannot be copied, keeping CJK words whole', async () => {
    writeText.mockRejectedValue(new Error('permission denied'));
    const passphrase = '山川 森林 海辺 夜空';
    show({ passphrase });
    expect(target.textContent).not.toContain(passphrase);
    button('created.passphrase.copy').click();
    await tick();
    const shown = [...target.querySelectorAll('span')].find((element) => element.textContent === passphrase);
    expect(shown?.classList).toContain('break-keep');
  });

  it.each([
    { keeps: true, note: 'created.track.note.kept' },
    { keeps: false, note: 'created.track.note' },
  ] as const)(
    'saves to Sent only when asked, with the typed label, and says where: keeps=$keeps',
    async ({ keeps, note }) => {
      show({ keeps });
      const labelField = () =>
        target.querySelector<HTMLInputElement>(
          `input[placeholder="${CSS.escape(translator.translate('created.track.placeholder'))}"]`,
        );
      expect(labelField()).toBeNull();
      button('created.track.title').click();
      await tick();
      const label = labelField();
      if (!label) {
        throw new Error('missing label field');
      }
      expect(document.activeElement).toBe(label);
      expect(target.textContent).toContain(translator.translate(note));
      expect(remember).not.toHaveBeenCalled();
      label.value = 'Password for Peter';
      label.dispatchEvent(new Event('input', { bubbles: true }));
      target.querySelector<HTMLButtonElement>('form button[type="submit"]')?.click();
      expect(remember).toHaveBeenCalledExactlyOnceWith('Password for Peter');
    },
  );

  it('offers no tracking when nothing can remember the message', () => {
    show({ trackable: false });
    expect([...target.querySelectorAll('button')].map((element) => element.textContent.trim())).not.toContain(
      translator.translate('created.track.title'),
    );
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
