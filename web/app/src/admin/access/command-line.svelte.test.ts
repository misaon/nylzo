import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { adminTranslator } from '../messages/catalog.ts';
import { getAdminTranslator } from '../messages/translator-context.ts';
import CommandLine from './CommandLine.svelte';

vi.mock(import('../messages/translator-context.ts'), (original) =>
  original().then((actual) => ({ ...actual, getAdminTranslator: vi.fn<typeof actual.getAdminTranslator>() })),
);

const COMMAND = 'nylzo admin-enroll';

describe('command line', () => {
  const { translate } = adminTranslator('en');
  const writeText = vi.fn<(text: string) => Promise<void>>();
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;

  function copyButton(): HTMLButtonElement {
    const button = target.querySelector('button');
    if (!button) {
      throw new Error('missing copy button');
    }
    return button;
  }

  function announced(): string | undefined {
    return target.querySelector('[role="status"]')?.textContent.trim();
  }

  async function copy(): Promise<void> {
    copyButton().click();
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(getAdminTranslator).mockReturnValue(adminTranslator('en'));
    writeText.mockReset().mockResolvedValue();
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    target = document.createElement('div');
    document.body.append(target);
    component = mount(CommandLine, { target, props: { label: 'Binary', command: COMMAND } });
    flushSync();
  });

  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.useRealTimers();
  });

  it('names the command it copies', () => {
    expect(copyButton().getAttribute('aria-label')).toBe(translate('access.copy.label', { command: COMMAND }));
    expect(announced()).toBe('');
  });

  it('copies the command and confirms it for two seconds', async () => {
    await copy();

    expect(writeText).toHaveBeenCalledWith(COMMAND);
    expect(copyButton().textContent.trim()).toBe(translate('access.copied'));
    expect(announced()).toBe(translate('access.copied'));

    await vi.advanceTimersByTimeAsync(2000);
    flushSync();
    expect(copyButton().textContent.trim()).toBe(translate('access.copy'));
    expect(announced()).toBe('');
  });

  it('withdraws an earlier confirmation when copying fails', async () => {
    await copy();
    writeText.mockRejectedValue(new DOMException('denied', 'NotAllowedError'));

    await copy();

    expect(copyButton().textContent.trim()).toBe(translate('access.copy'));
    expect(announced()).toBe('');
  });
});
