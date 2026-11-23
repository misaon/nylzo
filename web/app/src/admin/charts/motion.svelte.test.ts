import { flushSync } from 'svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { tweenedLayers, tweenedNumber, tweenedSeries } from './motion.svelte.ts';

const FRAME_MS = 16;
const SLOW_MS = 1_000_000;

type Values = readonly number[];

interface Frame {
  readonly layers: readonly Values[];
  readonly keys?: Values | undefined;
}

function tracked<Value, Shown>(initial: Value, create: (read: () => Value) => Shown) {
  const source = $state({ value: initial });
  let shown: Shown | undefined;
  const stop = $effect.root(() => {
    shown = create(() => source.value);
  });
  if (shown === undefined) {
    throw new Error('the tween was not created');
  }
  return {
    shown,
    stop,
    change(value: Value) {
      source.value = value;
      flushSync();
    },
  };
}

function rounded(layers: readonly Values[]): number[][] {
  return layers.map((values) => values.map((value) => Math.round(value * 100) / 100));
}

function startsOfChange(from: Frame, to: Frame, onlyLatestChanges = false): number[][] {
  const chart = tracked(from, (read) =>
    tweenedLayers(() => read().layers, { keys: () => read().keys, onlyLatestChanges, milliseconds: SLOW_MS }),
  );
  chart.change(to);
  vi.advanceTimersByTime(FRAME_MS);
  const starts = rounded(chart.shown.current);
  vi.advanceTimersByTime(SLOW_MS);
  expect(chart.shown.current).toEqual(to.layers);
  chart.stop();
  return starts;
}

describe('tweened values', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['requestAnimationFrame', 'performance'] });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('eases a number to its new value', () => {
    const number = tracked(0, (read) => tweenedNumber(read));
    number.change(100);
    vi.advanceTimersByTime(100);
    expect(number.shown.current).toBeGreaterThan(0);
    expect(number.shown.current).toBeLessThan(100);
    vi.advanceTimersByTime(1000);
    expect(number.shown.current).toBe(100);
    number.stop();
  });

  it('eases a single series without options and lines it up from the right', () => {
    const series = tracked<Values, { readonly current: Values }>([1, 2, 3], (read) => tweenedSeries(read));
    expect(series.shown.current).toEqual([1, 2, 3]);
    series.change([4, 5, 6, 7]);
    vi.advanceTimersByTime(1000);
    expect(series.shown.current).toEqual([4, 5, 6, 7]);
    series.stop();
  });

  it('starts values without keys from the old values counted from the right, and new ones from zero', () => {
    expect(startsOfChange({ layers: [[1, 2, 3]] }, { layers: [[5, 6, 7, 8]] })).toEqual([[0, 1, 2, 3]]);
    expect(startsOfChange({ layers: [[1, 2, 3]] }, { layers: [[9, 9]] })).toEqual([[2, 3]]);
  });

  it('starts a layer that was not there before from zero', () => {
    expect(
      startsOfChange(
        { layers: [[1, 2]] },
        {
          layers: [
            [3, 4],
            [5, 6],
          ],
        },
      ),
    ).toEqual([
      [1, 2],
      [0, 0],
    ]);
  });

  it('starts keyed values from the value of the same key and an unknown key from the value before it', () => {
    const from = { layers: [[1, 2, 3]], keys: [10, 11, 12] };
    expect(startsOfChange(from, { layers: [[5, 6, 7]], keys: [11, 12, 13] })).toEqual([[2, 3, 3]]);
    expect(startsOfChange(from, { layers: [[4, 4]], keys: [9, 11] })).toEqual([[0, 2]]);
    expect(startsOfChange(from, { layers: [[5, 6]], keys: [11] })).toEqual([[2, 2]]);
  });

  it('starts a key that had no value from zero', () => {
    expect(startsOfChange({ layers: [[1]], keys: [10, 11] }, { layers: [[4]], keys: [11] })).toEqual([[0]]);
  });

  it('lines values up from the right when no key is shared or a side has no keys', () => {
    const from = { layers: [[1, 2, 3]], keys: [10, 11, 12] };
    expect(startsOfChange(from, { layers: [[5, 6]], keys: [20, 21] })).toEqual([[2, 3]]);
    expect(startsOfChange(from, { layers: [[5, 6]] })).toEqual([[2, 3]]);
  });

  it('slides a moving window left and grows its new latest value from the last one', () => {
    expect(startsOfChange({ layers: [[1, 2, 3, 4]] }, { layers: [[2, 3, 4, 6]] }, true)).toEqual([[2, 3, 4, 4]]);
    expect(startsOfChange({ layers: [[1, 2, 3, 4]] }, { layers: [[1, 2, 3, 5]] }, true)).toEqual([[1, 2, 3, 4]]);
  });

  it('lines a window up from the right when no slide explains the change', () => {
    expect(startsOfChange({ layers: [[1, 2, 3, 4]] }, { layers: [[0, 0, 0, 0]] }, true)).toEqual([[1, 2, 3, 4]]);
    expect(startsOfChange({ layers: [[1, 2, 3]] }, { layers: [[3, 4]] }, true)).toEqual([[2, 3]]);
    expect(startsOfChange({ layers: [[]] }, { layers: [[]] }, true)).toEqual([[]]);
  });
});
