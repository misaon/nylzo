import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';

import SwapIcon from './SwapIcon.svelte';

describe('swap icon', () => {
  let component: ReturnType<typeof mount> | undefined;

  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    document.body.replaceChildren();
  });

  function icons(swapped: boolean): readonly SVGElement[] {
    const target = document.createElement('div');
    document.body.append(target);
    component = mount(SwapIcon, { target, props: { from: 'copy', to: 'check', swapped } });
    flushSync();
    return [...target.querySelectorAll('svg')];
  }

  it('shows the first icon until swapped, at the default size', () => {
    const [from, to] = icons(false);

    expect(from?.classList.contains('opacity-100')).toBe(true);
    expect(to?.classList.contains('opacity-0')).toBe(true);
    expect(from?.classList.contains('size-4')).toBe(true);
    expect(to?.classList.contains('size-4')).toBe(true);
  });

  it('shows the second icon once swapped', () => {
    const [from, to] = icons(true);

    expect(from?.classList.contains('opacity-0')).toBe(true);
    expect(to?.classList.contains('opacity-100')).toBe(true);
  });
});
