import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { loadWorldMap } from './world-map.ts';
import WorldMap from './WorldMap.svelte';

function pointer(element: Element, type: string, pointerType = 'mouse'): void {
  element.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerType }));
}

describe('world map', () => {
  let target: HTMLElement;
  let component: ReturnType<typeof mount> | undefined;
  const onhighlight = vi.fn<(country: string | undefined) => void>();
  const props = $state<{ highlighted: string | undefined }>({ highlighted: undefined });

  async function show(visitors: Readonly<Record<string, number>>): Promise<void> {
    component = mount(WorldMap, {
      target,
      props: {
        label: 'Visitors by country',
        visitors,
        get highlighted() {
          return props.highlighted;
        },
        onhighlight,
      },
    });
    await loadWorldMap();
    await Promise.resolve();
    flushSync();
  }

  function country(code: string): SVGPathElement {
    const path = target.querySelector<SVGPathElement>(`path[data-country="${code}"]`);
    if (!path) {
      throw new Error(`missing country ${code}`);
    }
    return path;
  }

  function outlines(): NodeListOf<SVGPathElement> {
    return target.querySelectorAll<SVGPathElement>('g.pointer-events-none path');
  }

  beforeEach(() => {
    onhighlight.mockReset();
    props.highlighted = undefined;
    target = document.createElement('div');
    document.body.append(target);
  });
  afterEach(() => {
    if (component) {
      void unmount(component);
    }
    target.remove();
  });

  it('draws every country and shades it by its share of the largest country', async () => {
    const world = await loadWorldMap();
    await show({ DE: 100, CZ: 1 });

    expect(target.querySelector('[role="img"]')?.getAttribute('aria-label')).toBe('Visitors by country');
    expect(target.querySelectorAll('path[data-country]')).toHaveLength(Object.keys(world.countries).length);
    expect(country('DE').getAttribute('class')).toContain('fill-aegis-300');
    expect(country('CZ').getAttribute('class')).toContain('fill-aegis-500/30');
    expect(country('FR').getAttribute('class')).toContain('fill-white/[0.045]');
  });

  it('highlights a country with visitors under the pointer and clears it when a mouse leaves', async () => {
    await show({ DE: 3 });

    pointer(country('DE'), 'pointerover');
    pointer(country('FR'), 'pointerover');
    pointer(country('DE').parentElement ?? target, 'pointerover');
    expect(onhighlight.mock.calls).toEqual([['DE'], [undefined]]);

    onhighlight.mockClear();
    pointer(country('DE').parentElement ?? target, 'pointerleave', 'touch');
    expect(onhighlight).not.toHaveBeenCalled();
    pointer(country('DE').parentElement ?? target, 'pointerleave');
    expect(onhighlight).toHaveBeenCalledExactlyOnceWith(undefined);
  });

  it('outlines the highlighted country only when the map knows it', async () => {
    await show({ DE: 3 });
    expect(outlines()).toHaveLength(0);

    props.highlighted = 'DE';
    flushSync();
    expect([...outlines()].map((path) => path.getAttribute('d'))).toEqual([
      country('DE').getAttribute('d'),
      country('DE').getAttribute('d'),
    ]);

    props.highlighted = 'unknown';
    flushSync();
    expect(outlines()).toHaveLength(0);
  });
});
