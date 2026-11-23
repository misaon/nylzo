import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../language/translator.svelte.ts';
import CopyButton from './CopyButton.svelte';

vi.mock(import('../../language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

type Shown = 'copy' | 'copied' | 'failed';

function shown(button: HTMLButtonElement): Shown {
  const [copy, copied, failed] = button.firstElementChild?.children ?? [];
  const visible = [
    ['copy', copy],
    ['copied', copied],
    ['failed', failed],
  ] as const;
  const found = visible.filter(([, element]) => element?.classList.contains('opacity-100'));
  if (found.length !== 1 || !found[0]) {
    throw new Error('exactly one state must be visible');
  }
  return found[0][0];
}

async function press(button: HTMLButtonElement): Promise<void> {
  button.click();
  await vi.advanceTimersByTimeAsync(0);
  flushSync();
}

describe('copy button', () => {
  const translator = new Translator('en');
  let component: ReturnType<typeof mount>;
  let target: HTMLElement;

  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(getTranslator).mockReturnValue(translator);
    target = document.createElement('div');
    document.body.append(target);
  });

  afterEach(async () => {
    await unmount(component);
    vi.useRealTimers();
    vi.restoreAllMocks();
    document.body.replaceChildren();
  });

  function show(label?: string): HTMLButtonElement {
    component = mount(CopyButton, { target, props: { text: 'secret link', label } });
    flushSync();
    const button = target.querySelector('button');
    if (!button) {
      throw new TypeError('the copy button did not render');
    }
    return button;
  }

  function status(): string {
    return target.querySelector('[role="status"]')?.textContent ?? '';
  }

  it('copies the text and confirms it for two seconds', async () => {
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
    const button = show();
    expect(button.getAttribute('aria-label')).toBe(translator.translate('action.copy'));
    expect(shown(button)).toBe('copy');

    await press(button);
    expect(writeText).toHaveBeenCalledExactlyOnceWith('secret link');
    expect(shown(button)).toBe('copied');
    expect(status()).toBe(translator.translate('action.copied'));

    await vi.advanceTimersByTimeAsync(2000);
    flushSync();
    expect(shown(button)).toBe('copy');
    expect(status()).toBe('');
  });

  it('reports a refused copy and clears it on the next success', async () => {
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValueOnce(new Error('denied'));
    const button = show('Copy the link');
    expect(button.getAttribute('aria-label')).toBe('Copy the link');

    await press(button);
    expect(shown(button)).toBe('failed');
    expect(status()).toBe(translator.translate('action.copy_failed'));

    writeText.mockResolvedValue();
    await press(button);
    expect(shown(button)).toBe('copied');
  });
});
