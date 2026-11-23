import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ColumnChart from './ColumnChart.svelte';
import type { ChartSeries } from './tones.ts';

const HEIGHT = 236;
const LEFT = 46;
const BOTTOM = 210;
// Svelte keeps one animation loop for all tweens, so each test lets its tweens finish on the fake clock.
const SETTLED_MS = 1000;

interface Chart {
  label: string;
  series: readonly ChartSeries[];
  keys?: readonly number[] | undefined;
  hint?: string | undefined;
  formatValue: (value: number) => string;
  formatTick: (value: number) => string;
  periodLabel: (first: number, last: number) => string;
  tickLabel: (index: number) => string;
  tickIndexes: (width: number) => readonly number[];
}

interface Column {
  readonly x: number;
  readonly y: number;
  readonly height: number;
  readonly tone: string;
  readonly dimmed: boolean;
}

const people: ChartSeries = { label: 'People', values: [2, 4, 0, 1], tone: 'people' };
const robots: ChartSeries = { label: 'Robots', values: [1, 0, 0, 1], tone: 'robots' };

let width = 606;
let target: HTMLElement;
let component: ReturnType<typeof mount> | undefined;

function show(changes: Partial<Chart> = {}): Chart {
  const props: Chart = $state({
    label: 'Messages',
    series: [people, robots],
    keys: [10, 11, 12, 13],
    formatValue: (value) => `${value}`,
    formatTick: (value) => `${value}`,
    periodLabel: (first, last) => `P${first}-${last}`,
    tickLabel: (index) => `T${index}`,
    tickIndexes: () => [0, 1, 2, 3],
    ...changes,
  });
  component = mount(ColumnChart, { target, props });
  flushSync();
  vi.advanceTimersByTime(SETTLED_MS);
  flushSync();
  return props;
}

function slider(): HTMLElement {
  const element = target.querySelector<HTMLElement>('[role="slider"]');
  if (!element) {
    throw new Error('missing slider');
  }
  return element;
}

function send(event: Event): Event {
  slider().dispatchEvent(event);
  flushSync();
  return event;
}

function press(key: string): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key, cancelable: true, bubbles: true });
  send(event);
  return event;
}

function columns(): Column[] {
  return [...target.querySelectorAll('rect')]
    .filter((rect) => rect.getAttribute('rx') !== '4')
    .map((rect) => ({
      x: Math.round(Number(rect.getAttribute('x'))),
      y: Math.round(Number(rect.getAttribute('y'))),
      height: Math.round(Number(rect.getAttribute('height'))),
      tone: rect.classList.contains('fill-aegis-400') ? 'people' : 'robots',
      dimmed: rect.classList.contains('opacity-50'),
    }));
}

function highlight(): number | undefined {
  const band = target.querySelector('rect[rx="4"]');
  return band ? Math.round(Number(band.getAttribute('x'))) : undefined;
}

function tooltip(): { title: string; rows: string[] } | undefined {
  const box = target.querySelector('foreignObject');
  if (!box) {
    return undefined;
  }
  return {
    title: box.querySelector('p')?.textContent ?? '',
    rows: [...box.querySelectorAll('li')].map((row) => row.textContent.replaceAll(/\s+/gu, '')),
  };
}

function axisLabels(): { text: string; anchor: string | null; active: boolean }[] {
  return [...target.querySelectorAll('text')]
    .filter((text) => text.textContent.trim().startsWith('T'))
    .map((text) => ({
      text: text.textContent.trim(),
      anchor: text.getAttribute('text-anchor'),
      active: text.classList.contains('fill-ink'),
    }));
}

function gridLines(): { y: number; dashed: boolean }[] {
  return [...target.querySelectorAll('line')].map((line) => ({
    y: Number(line.getAttribute('y1')),
    dashed: line.hasAttribute('stroke-dasharray'),
  }));
}

beforeEach(() => {
  width = 606;
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'performance'] });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => width);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(HEIGHT);
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
  vi.restoreAllMocks();
});

describe('column chart', () => {
  it('stacks the layers of each period with a gap between them and leaves out empty segments', () => {
    show();
    expect(columns()).toEqual([
      { x: 71, y: 110, height: 100, tone: 'people', dimmed: false },
      { x: 71, y: 60, height: 49, tone: 'robots', dimmed: false },
      { x: 209, y: 10, height: 200, tone: 'people', dimmed: false },
      { x: 486, y: 160, height: 50, tone: 'people', dimmed: false },
      { x: 486, y: 110, height: 49, tone: 'robots', dimmed: false },
    ]);
    expect(slider().getAttribute('aria-valuemax')).toBe('3');
    expect(slider().getAttribute('aria-valuetext')).toBe('P3-3: People 1, Robots 1');
  });

  it('draws the zero line solid and the other grid lines dashed', () => {
    show();
    expect(gridLines()).toEqual([
      { y: BOTTOM, dashed: false },
      { y: 160, dashed: true },
      { y: 110, dashed: true },
      { y: 60, dashed: true },
      { y: 10, dashed: true },
    ]);
  });

  it('counts a layer that ends early as zero in the later periods', () => {
    show({ series: [people, { ...robots, values: [1] }] });
    expect(slider().getAttribute('aria-valuetext')).toBe('P3-3: People 1, Robots 0');
  });

  it('groups periods into wider columns when the plot is too narrow and labels each group once', () => {
    width = 106;
    show({
      series: [{ ...people, values: [1, 2, 3, 4, 5, 6, 7, 8, 9] }],
      tickIndexes: () => [0, 1, 4, 5],
    });
    expect(columns()).toHaveLength(5);
    expect(axisLabels().map(({ text }) => text)).toEqual(['T0', 'T4']);
    expect(slider().getAttribute('aria-valuetext')).toBe('P8-8: People 9');
    press('ArrowLeft');
    expect(tooltip()).toEqual({ title: 'P6-7', rows: ['People15'] });
  });

  it('keeps the labels of the first and last columns inside the plot', () => {
    show({
      series: [{ ...people, values: Array.from({ length: 20 }, () => 1) }],
      keys: undefined,
      tickIndexes: () => [0, 10, 19],
    });
    expect(axisLabels().map(({ anchor }) => anchor)).toEqual(['start', 'middle', 'end']);
  });

  it('picks the column under the pointer, dims the others and keeps it for touch until cancelled', () => {
    show();
    send(new PointerEvent('pointermove', { bubbles: true, clientX: LEFT + 1.5 * 138.5 }));
    expect(highlight()).toBe(185);
    expect(tooltip()).toEqual({ title: 'P1-1', rows: ['People4', 'Robots0'] });
    expect(
      axisLabels()
        .filter(({ active }) => active)
        .map(({ text }) => text),
    ).toEqual(['T1']);
    expect(columns().map(({ dimmed }) => dimmed)).toEqual([true, true, false, true, true]);

    send(new PointerEvent('pointerdown', { bubbles: true, clientX: LEFT }));
    send(new PointerEvent('pointerleave', { pointerType: 'touch' }));
    expect(tooltip()?.title).toBe('P0-0');
    send(new PointerEvent('pointercancel'));
    expect(tooltip()).toBeUndefined();
    expect(highlight()).toBeUndefined();

    send(new PointerEvent('pointermove', { bubbles: true, clientX: 590 }));
    expect(tooltip()?.title).toBe('P3-3');
    send(new PointerEvent('pointerleave', { pointerType: 'mouse' }));
    expect(tooltip()).toBeUndefined();
  });

  it('starts at the latest column on focus and steps with the keyboard', () => {
    show();
    send(new FocusEvent('focus'));
    expect(tooltip()?.title).toBe('P3-3');
    expect(press('ArrowLeft').defaultPrevented).toBe(true);
    expect(tooltip()?.title).toBe('P2-2');
    expect(press('a').defaultPrevented).toBe(false);
    press('Escape');
    expect(tooltip()).toBeUndefined();
    press('Home');
    expect(tooltip()?.title).toBe('P0-0');
    send(new FocusEvent('blur'));
    expect(tooltip()).toBeUndefined();
  });

  it('shows a picked column the new values no longer reach as empty', () => {
    const props = show();
    press('End');
    props.series = [{ ...people, values: [5, 6] }];
    flushSync();
    expect(tooltip()?.rows).toEqual(['People0']);
    expect(slider().getAttribute('aria-valuetext')).toBe('P1-1: People 6');
  });

  it('grows a new layer and a higher scale in from the old chart', () => {
    const props = show({ hint: 'Counting since today' });
    expect(target.querySelector('p')?.textContent.trim()).toBe('Counting since today');
    props.series = [people, robots, { label: 'Ended', values: [8, 8, 8, 8], tone: 'ended' }];
    flushSync();
    expect(columns()).toHaveLength(5);
    expect(gridLines().map(({ y }) => y)).toEqual([BOTTOM]);
    vi.advanceTimersByTime(SETTLED_MS);
    flushSync();
    expect(columns()).toHaveLength(9);
    expect(gridLines()).toHaveLength(4);
  });

  it('names only the chart while it has no series and shows no tooltip', () => {
    show({ series: [] });
    expect(slider().getAttribute('aria-valuetext')).toBe('Messages');
    expect(slider().getAttribute('aria-valuenow')).toBe('0');
    send(new FocusEvent('focus'));
    expect(tooltip()).toBeUndefined();
    expect(highlight()).toBeUndefined();
  });
});
