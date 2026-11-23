import { flushSync, mount, unmount } from 'svelte';
import type { ComponentProps } from 'svelte';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import StatusOrb from './StatusOrb.svelte';

describe('status orb', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;

  function show(props: ComponentProps<typeof StatusOrb>): void {
    component = mount(StatusOrb, { target, props });
    flushSync();
  }

  beforeEach(() => {
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(async () => {
    if (component) {
      await unmount(component);
    }
    target.remove();
  });

  it.each([
    { glyph: 'compass', shapes: ['circle', 'g'] },
    { glyph: 'server', shapes: ['rect', 'rect', 'path', 'circle', 'circle'] },
    { glyph: 'wrench', shapes: ['g'] },
  ] as const)('draws the $glyph glyph', ({ glyph, shapes }) => {
    show({ tone: 'current', glyph, class: 'size-10' });
    expect([...(target.querySelector('svg')?.children ?? [])].map((shape) => shape.tagName.toLowerCase())).toEqual(
      shapes,
    );
  });

  it.each([
    { orbit: false, circled: false },
    { orbit: true, circled: true },
  ])('circles the orb with an orbit only when asked ($orbit)', ({ orbit, circled }) => {
    show({ tone: 'warning', glyph: 'wrench', class: 'size-10', orbit });
    expect(target.querySelector('[class*="animate-orbit"]') !== null).toBe(circled);
  });
});
