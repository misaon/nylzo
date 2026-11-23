import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import AnimatedNumber from './AnimatedNumber.svelte';
import BarList from './BarList.svelte';
import ShareBar from './ShareBar.svelte';
import Sparkline from './Sparkline.svelte';
import type { BarItem } from './tones.ts';
import Track from './Track.svelte';

const format = (value: number) => `${Math.round(value)}`;
const percent = (share: number) => `${Math.round(share * 100)}%`;

// Svelte keeps one animation loop for all tweens, so each test lets its tweens finish on the fake clock.
const SETTLED_MS = 1000;

let target: HTMLElement;
let component: ReturnType<typeof mount> | undefined;

function show(mountComponent: () => ReturnType<typeof mount>): void {
  component = mountComponent();
  flushSync();
}

function rectangles(): { x: string | null; width: string | null; className: string }[] {
  return [...target.querySelectorAll('rect')].map((rect) => ({
    x: rect.getAttribute('x'),
    width: rect.getAttribute('width'),
    className: rect.getAttribute('class') ?? '',
  }));
}

function tracks(): number[] {
  return [...target.querySelectorAll('rect')].map((rect) => Math.round(Number(rect.getAttribute('width'))));
}

function paths(): (string | null)[] {
  return [...target.querySelectorAll('path')].map((path) => path.getAttribute('d'));
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'performance'] });
  target = document.createElement('div');
  document.body.append(target);
});

afterEach(async () => {
  if (component) {
    await unmount(component);
    component = undefined;
  }
  target.remove();
  vi.advanceTimersByTime(SETTLED_MS);
  vi.useRealTimers();
});

describe('animated number', () => {
  it('eases the visible number while screen readers get the new value at once', () => {
    const props = $state({ value: 10, format });
    show(() => mount(AnimatedNumber, { target, props }));
    const [visible, spoken] = target.querySelectorAll('span');
    expect(visible?.getAttribute('aria-hidden')).toBe('true');
    expect(visible?.textContent).toBe('10');

    props.value = 1010;
    flushSync();
    vi.advanceTimersByTime(100);
    flushSync();
    expect(spoken?.textContent).toBe('1010');
    expect(Number(visible?.textContent)).toBeGreaterThan(10);
    expect(Number(visible?.textContent)).toBeLessThan(1010);

    vi.advanceTimersByTime(1000);
    flushSync();
    expect(visible?.textContent).toBe('1010');
  });
});

describe('track', () => {
  it('fills the share of the value, clamped to the track', () => {
    const props = $state({ value: 0.25, tone: 'people' as const });
    show(() => mount(Track, { target, props }));
    expect(rectangles()).toEqual([{ x: null, width: '250', className: 'fill-aegis-400' }]);

    props.value = 3;
    flushSync();
    vi.advanceTimersByTime(1000);
    flushSync();
    expect(rectangles()[0]?.width).toBe('1000');
  });

  it('marks the reserve at the end of the track', () => {
    show(() => mount(Track, { target, props: { value: 0.5, tone: 'danger', reserve: 0.2 } }));
    expect(rectangles().map(({ x, width }) => ({ x, width }))).toEqual([
      { x: '800', width: '200' },
      { x: null, width: '500' },
      { x: '797', width: '6' },
    ]);
  });

  it('draws no reserve when there is none', () => {
    show(() => mount(Track, { target, props: { value: 0.5, tone: 'danger', reserve: 0 } }));
    expect(rectangles()).toHaveLength(1);
  });
});

describe('share bar', () => {
  const parts = [
    { label: 'People', tone: 'people' },
    { label: 'Robots', tone: 'robots' },
    { label: 'Ended', tone: 'ended' },
  ] as const;

  it('splits the bar by share with a gap between the parts and lists each value with its share', () => {
    show(() =>
      mount(ShareBar, {
        target,
        props: { label: 'Visits', parts, values: [3, 1], formatValue: format, formatShare: percent },
      }),
    );
    expect(rectangles().map(({ x, width }) => ({ x, width }))).toEqual([
      { x: '0', width: '748' },
      { x: '752', width: '246' },
      { x: '1002', width: '0' },
    ]);
    expect(target.querySelector('ul')?.getAttribute('aria-label')).toBe('Visits');
    const figures = [...target.querySelectorAll('li')].map((item) => item.textContent.replaceAll(/\s+/gu, ' ').trim());
    expect(figures).toEqual(['People 3 · 75%', 'Robots 1 · 25%', 'Ended 0 · 0%']);
  });
});

describe('bar list', () => {
  const items: BarItem[] = [
    { key: 'cz', label: 'Czechia', value: 6 },
    { key: 'de', label: 'Germany', value: 2 },
  ];

  it('sizes each bar against the largest and shows its share of the total', () => {
    show(() =>
      mount(BarList, {
        target,
        props: { label: 'Countries', items, tone: 'people', formatValue: format, formatShare: percent },
      }),
    );
    expect(target.querySelector('button')).toBeNull();
    expect(tracks()).toEqual([1000, 333]);
    expect([...target.querySelectorAll('li')].map((item) => item.textContent.replaceAll(/\s+/gu, ''))).toEqual([
      'Czechia675%',
      'Germany225%',
    ]);
  });

  it('shows empty bars and no share when nothing was counted', () => {
    show(() =>
      mount(BarList, {
        target,
        props: {
          label: 'Countries',
          items: [{ key: 'cz', label: 'Czechia', value: 0 }],
          tone: 'people',
          formatValue: format,
          formatShare: percent,
          compact: true,
        },
      }),
    );
    expect(tracks()).toEqual([0]);
    expect(target.querySelector('li')?.textContent).toContain('0%');
  });

  it('highlights an item on click, hover and focus and lets go when a mouse leaves or the focus moves on', () => {
    const onhighlight = vi.fn<(key: string | undefined) => void>();
    const selection: { highlighted: string | undefined } = $state({ highlighted: undefined });
    show(() =>
      mount(BarList, {
        target,
        props: {
          label: 'Countries',
          items,
          tone: 'people',
          formatValue: format,
          formatShare: percent,
          get highlighted() {
            return selection.highlighted;
          },
          onhighlight,
        },
      }),
    );
    const [first, second] = target.querySelectorAll('button');
    if (!first || !second) {
      throw new Error('missing buttons');
    }
    first.click();
    second.dispatchEvent(new PointerEvent('pointerenter'));
    first.dispatchEvent(new FocusEvent('focus'));
    first.dispatchEvent(new PointerEvent('pointerleave', { pointerType: 'touch' }));
    first.dispatchEvent(new PointerEvent('pointerleave', { pointerType: 'mouse' }));
    first.dispatchEvent(new FocusEvent('blur'));
    expect(onhighlight.mock.calls).toEqual([['cz'], ['de'], ['cz'], [undefined], [undefined]]);

    selection.highlighted = 'de';
    flushSync();
    expect(second.classList.contains('bg-white/8')).toBe(true);
    expect(first.classList.contains('bg-white/8')).toBe(false);
  });
});

describe('sparkline', () => {
  it('scales the area to its largest value and draws no overlay without one', () => {
    show(() => mount(Sparkline, { target, props: { values: [0, 10, -5], tone: 'people' } }));
    expect(paths()).toEqual([
      'M0,37C16.67,20 33.33,3 50,3C66.67,3 83.33,20 100,37L100,37L0,37Z',
      'M0,37C16.67,20 33.33,3 50,3C66.67,3 83.33,20 100,37',
    ]);
  });

  it('draws the overlay against a given maximum', () => {
    show(() =>
      mount(Sparkline, {
        target,
        props: { values: [5, 5], overlay: [10, 0], maximum: 20, keys: [1, 2], tone: 'people' },
      }),
    );
    expect(paths()).toEqual(['M0,28.5L100,28.5L100,37L0,37Z', 'M0,20L100,37', 'M0,28.5L100,28.5']);
  });

  it('takes the overlay into the scale when no maximum is given', () => {
    show(() => mount(Sparkline, { target, props: { values: [5, 5], overlay: [10, 0], tone: 'people' } }));
    expect(paths()[1]).toBe('M0,3L100,37');
  });
});
