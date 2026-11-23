import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../language/translator.svelte.ts';
import WorkingStatus from './WorkingStatus.svelte';

vi.mock(import('../language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

describe('working status', () => {
  let component: ReturnType<typeof mount> | undefined;

  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    document.body.replaceChildren();
  });

  function show(shown: boolean): HTMLElement {
    vi.mocked(getTranslator).mockReturnValue(new Translator('en'));
    const target = document.createElement('div');
    document.body.append(target);
    component = mount(WorkingStatus, { target, props: { label: 'Sealing', icon: 'lock', progress: 0.42, shown } });
    flushSync();
    const status = target.firstElementChild;
    if (!(status instanceof HTMLElement)) {
      throw new TypeError('the status rendered nothing');
    }
    return status;
  }

  it('shows the progress as a percentage while working', () => {
    const status = show(true);

    expect(status.getAttribute('aria-hidden')).toBe('false');
    expect(status.dataset['paused']).toBeUndefined();
    expect(status.querySelector('progress')?.value).toBe(0.42);
    expect(status.lastElementChild?.lastElementChild?.textContent).toBe('42%');
  });

  it('hides and pauses itself when not shown', () => {
    const status = show(false);

    expect(status.getAttribute('aria-hidden')).toBe('true');
    expect(status.dataset['paused']).toBe('true');
    expect(status.classList.contains('pointer-events-none')).toBe(true);
  });
});
