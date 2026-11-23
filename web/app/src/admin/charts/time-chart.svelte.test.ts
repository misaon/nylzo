import { flushSync, mount, unmount } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Ticks } from './geometry/scale.ts';
import TimeChart from './TimeChart.svelte';
import type { ChartSeries } from './tones.ts';

const WIDTH = 606;
const HEIGHT = 236;
const LEFT = 46;
const RIGHT = 600;
const BOTTOM = 210;

interface Chart {
  label: string;
  area: ChartSeries;
  line?: ChartSeries | undefined;
  keys?: readonly number[] | undefined;
  ticks?: Ticks | undefined;
  hint?: string | undefined;
  formatValue: (value: number) => string;
  formatTick: (value: number) => string;
  periodLabel: (index: number) => string;
  tickLabel: (index: number) => string;
  tickIndexes: (width: number) => readonly number[];
}

const people: ChartSeries = { label: 'People', values: [1, 2, 3, 4, 5], tone: 'people' };
const robots: ChartSeries = { label: 'Robots', values: [0, 1, 0, 1, 1], tone: 'robots' };

// Svelte keeps one animation loop for all tweens, so each test lets its tweens finish on the fake clock.
const SETTLED_MS = 1000;

let target: HTMLElement;
let component: ReturnType<typeof mount> | undefined;

function show(changes: Partial<Chart> = {}): Chart {
  const props: Chart = $state({
    label: 'Visits',
    area: people,
    line: robots,
    formatValue: (value) => `${value}`,
    formatTick: (value) => `${value}`,
    periodLabel: (index) => `P${index}`,
    tickLabel: (index) => `T${index}`,
    tickIndexes: () => [0, 2, 4],
    ...changes,
  });
  component = mount(TimeChart, { target, props });
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

function tooltip(): { title: string; rows: string[]; justify: string; x: number } | undefined {
  const box = target.querySelector('foreignObject');
  if (!box) {
    return undefined;
  }
  return {
    title: box.querySelector('p')?.textContent ?? '',
    rows: [...box.querySelectorAll('li')].map((row) => row.textContent.replaceAll(/\s+/gu, '')),
    justify: box.firstElementChild?.classList.contains('justify-end') ? 'end' : 'start',
    x: Number(box.getAttribute('x')),
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
  return [...target.querySelectorAll('line')]
    .filter((line) => line.getAttribute('x1') === String(LEFT) && line.getAttribute('x2') === String(RIGHT))
    .map((line) => ({ y: Number(line.getAttribute('y1')), dashed: line.hasAttribute('stroke-dasharray') }));
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'performance'] });
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(WIDTH);
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

describe('time chart', () => {
  it('describes the latest period to assistive technology while nothing is picked', () => {
    show();
    expect(slider().getAttribute('aria-valuemax')).toBe('4');
    expect(slider().getAttribute('aria-valuenow')).toBe('4');
    expect(slider().getAttribute('aria-valuetext')).toBe('P4: People 5, Robots 1');
    expect(tooltip()).toBeUndefined();
  });

  it('labels the axis inside the plot and draws the zero line solid', () => {
    show({ ticks: { max: 10, values: [0, 5, 10] } });
    expect(axisLabels()).toEqual([
      { text: 'T0', anchor: 'start', active: false },
      { text: 'T2', anchor: 'middle', active: false },
      { text: 'T4', anchor: 'end', active: false },
    ]);
    expect(gridLines()).toEqual([
      { y: BOTTOM, dashed: false },
      { y: 110, dashed: true },
      { y: 10, dashed: true },
    ]);
  });

  it('shows the period under the pointer and keeps it for touch until the pointer is cancelled', () => {
    show();
    send(new PointerEvent('pointermove', { bubbles: true, clientX: LEFT + 140 }));
    expect(tooltip()).toEqual({ title: 'P1', rows: ['People2', 'Robots1'], justify: 'start', x: LEFT + 138.5 + 14 });
    expect(axisLabels().find((label) => label.active)).toBeUndefined();
    expect(slider().getAttribute('aria-valuetext')).toBe('P1: People 2, Robots 1');

    send(new PointerEvent('pointerdown', { bubbles: true, clientX: LEFT }));
    expect(axisLabels().find((label) => label.active)?.text).toBe('T0');
    send(new PointerEvent('pointerleave', { pointerType: 'touch' }));
    expect(tooltip()?.title).toBe('P0');
    send(new PointerEvent('pointercancel'));
    expect(tooltip()).toBeUndefined();

    send(new PointerEvent('pointermove', { bubbles: true, clientX: RIGHT }));
    expect(tooltip()).toMatchObject({ title: 'P4', justify: 'end', x: RIGHT - 14 - 224 });
    send(new PointerEvent('pointerleave', { pointerType: 'mouse' }));
    expect(tooltip()).toBeUndefined();
  });

  it('starts at the latest period on focus and steps with the keyboard', () => {
    show();
    send(new FocusEvent('focus'));
    expect(tooltip()?.title).toBe('P4');
    expect(press('ArrowLeft').defaultPrevented).toBe(true);
    expect(tooltip()?.title).toBe('P3');
    expect(press('a').defaultPrevented).toBe(false);
    expect(tooltip()?.title).toBe('P3');
    press('Escape');
    expect(tooltip()).toBeUndefined();
    press('Home');
    expect(tooltip()?.title).toBe('P0');
    send(new FocusEvent('blur'));
    expect(tooltip()).toBeUndefined();
    press('End');
    expect(tooltip()?.title).toBe('P4');
  });

  it('shows a picked period the new values no longer reach as empty', () => {
    const props = show();
    press('End');
    props.area = { ...people, values: [7, 8] };
    props.line = { ...robots, values: [3] };
    flushSync();
    expect(tooltip()?.rows).toEqual(['People0', 'Robots0']);
    expect(slider().getAttribute('aria-valuetext')).toBe('P1: People 8, Robots 0');
  });

  it('draws a single area without a line and centres a single period', () => {
    show({ area: { ...people, values: [4] }, line: undefined, hint: 'Not enough data yet' });
    expect(target.querySelectorAll('path')).toHaveLength(3);
    expect(target.querySelector('p')?.textContent.trim()).toBe('Not enough data yet');
    send(new FocusEvent('focus'));
    expect(tooltip()?.rows).toEqual(['People4']);
    expect(target.querySelectorAll('circle')).toHaveLength(2);
    expect(target.querySelector('circle')?.getAttribute('cx')).toBe(String((LEFT + RIGHT) / 2));
  });

  it('names only the chart while it has no periods and shows no tooltip', () => {
    show({ area: { ...people, values: [] }, line: undefined });
    expect(slider().getAttribute('aria-valuetext')).toBe('Visits');
    expect(slider().getAttribute('aria-valuemax')).toBe('0');
    expect(slider().getAttribute('aria-valuenow')).toBe('0');
    send(new FocusEvent('focus'));
    expect(tooltip()).toBeUndefined();
  });

  it('reveals higher grid lines only once the scale has grown to them', () => {
    const props = show({ line: undefined });
    expect(gridLines()).toHaveLength(4);
    props.area = { ...people, values: [1, 2, 3, 4, 40] };
    flushSync();
    expect(gridLines().map(({ y }) => y)).toEqual([BOTTOM]);
    vi.advanceTimersByTime(1000);
    flushSync();
    expect(gridLines()).toHaveLength(5);
  });
});
