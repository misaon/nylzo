import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';

import Switch from './Switch.svelte';

function thumb(track: HTMLElement): Element | undefined {
  return track.children[2];
}

describe('switch', () => {
  let component: ReturnType<typeof mount> | undefined;

  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    document.body.replaceChildren();
  });

  function show(props: { readonly on: boolean; readonly size?: 'small' | 'large' }): HTMLElement {
    const target = document.createElement('div');
    document.body.append(target);
    component = mount(Switch, { target, props });
    flushSync();
    const track = target.firstElementChild;
    if (!(track instanceof HTMLElement)) {
      throw new TypeError('the switch rendered no track');
    }
    return track;
  }

  it('slides the thumb across and lights the track when on', () => {
    const track = show({ on: true });

    expect(track.children[1]?.classList.contains('opacity-100')).toBe(true);
    expect(thumb(track)?.classList.contains('translate-x-4')).toBe(true);
    expect(thumb(track)?.lastElementChild?.classList.contains('opacity-100')).toBe(true);
  });

  it('keeps the thumb at the start and the track dark when off', () => {
    const track = show({ on: false });

    expect(track.children[1]?.classList.contains('opacity-0')).toBe(true);
    expect(thumb(track)?.classList.contains('translate-x-0')).toBe(true);
    expect(thumb(track)?.lastElementChild?.classList.contains('opacity-0')).toBe(true);
  });

  it('slides a large thumb further', () => {
    const track = show({ on: true, size: 'large' });

    expect(track.classList.contains('w-12')).toBe(true);
    expect(thumb(track)?.classList.contains('translate-x-5')).toBe(true);
  });
});
