import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MessageKey } from '../../language/catalog.ts';
import { getTranslator, Translator } from '../../language/translator.svelte.ts';
import CheckList from './CheckList.svelte';

vi.mock(import('../../language/translator.svelte.ts'), (original) =>
  original().then((actual) => ({ ...actual, getTranslator: vi.fn<typeof actual.getTranslator>() })),
);

describe('check list', () => {
  let component: ReturnType<typeof mount> | undefined;

  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    document.body.replaceChildren();
  });

  it('lists each item in the reader language', () => {
    const translator = new Translator('en');
    vi.mocked(getTranslator).mockReturnValue(translator);
    const items: readonly MessageKey[] = ['action.copy', 'action.copied'];
    const target = document.createElement('div');
    document.body.append(target);

    component = mount(CheckList, { target, props: { items } });
    flushSync();

    expect([...target.querySelectorAll('li')].map((item) => item.textContent.trim())).toEqual(
      items.map((item) => translator.translate(item)),
    );
  });
});
