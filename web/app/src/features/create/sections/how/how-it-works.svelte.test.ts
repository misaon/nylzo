import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { getTranslator, Translator } from '../../../../shared/language/translator.svelte.ts';
import HowItWorks from './HowItWorks.svelte';
import { LINK_PARTS } from './link-parts.ts';

vi.mock(import('../../../../shared/language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

describe('how a link is built', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount>;

  beforeEach(() => {
    vi.mocked(getTranslator).mockReturnValue(new Translator('en'));
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    await unmount(component);
    target.remove();
  });

  it('explains the part of the link the reader selects, starting with the last one', () => {
    component = mount(HowItWorks, { target });
    flushSync();
    const parts = [...target.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')];
    expect(parts).toHaveLength(LINK_PARTS.length);
    const pressed = () => parts.map((part) => part.getAttribute('aria-pressed'));
    expect(pressed().lastIndexOf('true')).toBe(LINK_PARTS.length - 1);
    parts[0]?.click();
    flushSync();
    expect(pressed()).toEqual(LINK_PARTS.map((_part, index) => String(index === 0)));
  });
});
