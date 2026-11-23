import { describe, expect, it } from 'vitest';

import { bandIndex, nearestIndex, steppedIndex } from './pointer.ts';

describe('pointer on a line chart', () => {
  it('picks the nearest point and clamps positions outside the plot', () => {
    expect(nearestIndex({ position: 60, start: 10, width: 100, count: 3 })).toBe(1);
    expect(nearestIndex({ position: 0, start: 10, width: 100, count: 3 })).toBe(0);
    expect(nearestIndex({ position: 500, start: 10, width: 100, count: 3 })).toBe(2);
  });

  it.each([
    { count: 1, width: 100 },
    { count: 5, width: 0 },
  ])('stays on the first point with $count points over a width of $width', (axis) => {
    expect(nearestIndex({ position: 80, start: 0, ...axis })).toBe(0);
  });
});

describe('pointer on a column chart', () => {
  it('picks the band under the pointer and clamps positions outside the plot', () => {
    expect(bandIndex({ position: 35, start: 10, width: 100, count: 4 })).toBe(1);
    expect(bandIndex({ position: -20, start: 10, width: 100, count: 4 })).toBe(0);
    expect(bandIndex({ position: 110, start: 10, width: 100, count: 4 })).toBe(3);
  });

  it.each([
    { count: 1, width: 100 },
    { count: 4, width: -1 },
  ])('stays on the first band with $count bands over a width of $width', (axis) => {
    expect(bandIndex({ position: 80, start: 0, ...axis })).toBe(0);
  });
});

describe('keyboard steps', () => {
  it.each([
    { key: 'ArrowLeft', expected: 4 },
    { key: 'ArrowDown', expected: 4 },
    { key: 'ArrowRight', expected: 6 },
    { key: 'ArrowUp', expected: 6 },
    { key: 'PageDown', expected: 0 },
    { key: 'PageUp', expected: 9 },
    { key: 'Home', expected: 0 },
    { key: 'End', expected: 9 },
    { key: 'Enter', expected: undefined },
  ])('moves $key from the sixth of ten periods to $expected', ({ key, expected }) => {
    expect(steppedIndex({ key, index: 5, count: 10 })).toBe(expected);
  });

  it('keeps an empty chart at the first index', () => {
    expect(steppedIndex({ key: 'End', index: 0, count: 0 })).toBe(0);
  });
});
