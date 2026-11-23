import { flushSync, mount, tick, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../../../shared/language/translator.svelte.ts';
import SelfHost from './SelfHost.svelte';

vi.mock(import('../../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

class RecordingObserver {
  readonly observed = new Set<Element>();

  observe(element: Element): void {
    this.observed.add(element);
  }

  unobserve(element: Element): void {
    this.observed.delete(element);
  }

  disconnect(): void {
    this.observed.clear();
  }
}

describe('self-hosting section', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;
  const translator = new Translator('en');
  const writeText = vi.fn<(text: string) => Promise<void>>();

  function tab(id: string): HTMLButtonElement {
    const found = target.querySelector<HTMLButtonElement>(`#host-tab-${id}`);
    if (!found) {
      throw new Error(`missing tab ${id}`);
    }
    return found;
  }
  function visibleCommands(): string {
    return target.querySelector('#host-panel > [aria-hidden="false"]')?.textContent.replaceAll('\u{A0}', ' ') ?? '';
  }
  async function copied(): Promise<string | undefined> {
    target.querySelector<HTMLButtonElement>(`button[aria-label="${translator.translate('host.copy.label')}"]`)?.click();
    await tick();
    return writeText.mock.lastCall?.[0];
  }

  beforeEach(() => {
    vi.mocked(getTranslator).mockReturnValue(translator);
    writeText.mockResolvedValue();
    vi.stubGlobal('IntersectionObserver', RecordingObserver);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    target = document.createElement('div');
    document.body.append(target);
    component = mount(SelfHost, { target });
    flushSync();
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
    vi.unstubAllGlobals();
  });

  it('copies exactly the commands of the chosen tab, joining an environment assignment', async () => {
    expect(await copied()).toBe(visibleCommands().replaceAll('$ ', '\n').trim());
    tab('binary').click();
    flushSync();
    expect(tab('binary').getAttribute('aria-selected')).toBe('true');
    const binary = await copied();
    expect(binary).toContain('NYLZO_PEPPER_FILE=pepper nylzo serve');
    expect(visibleCommands()).toContain('NYLZO_PEPPER_FILE=pepper nylzo serve');
    tab('verify').click();
    flushSync();
    expect(await copied()).toContain(`--server https://${translator.translate('host.domain')}`);
  });

  it('moves between tabs with the arrow keys and ignores other keys', () => {
    tab('cli').focus();
    tab('cli').dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true, cancelable: true }));
    flushSync();
    expect(tab('cli').getAttribute('aria-selected')).toBe('true');
    const left = new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true, cancelable: true });
    tab('cli').dispatchEvent(left);
    flushSync();
    expect(left.defaultPrevented).toBe(true);
    expect(tab('verify').getAttribute('aria-selected')).toBe('true');
    expect(tab('verify').tabIndex).toBe(0);
    expect(tab('cli').tabIndex).toBe(-1);
    expect(document.activeElement).toBe(tab('verify'));
    tab('verify').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }));
    flushSync();
    expect(document.activeElement).toBe(tab('cli'));
    tab('compose').click();
    flushSync();
    expect(visibleCommands()).toContain('docker compose up -d');
  });
});
