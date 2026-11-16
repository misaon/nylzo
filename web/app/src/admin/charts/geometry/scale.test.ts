import { describe, expect, it } from 'vitest';

import { groupSeries, level, niceTicks } from './scale.ts';

describe('nice ticks', () => {
  it.each([
    { maximum: 0, expected: { max: 1, values: [0] } },
    { maximum: Number.NaN, expected: { max: 1, values: [0] } },
    { maximum: 3, expected: { max: 3, values: [0, 1, 2, 3] } },
    { maximum: 37, expected: { max: 40, values: [0, 10, 20, 30, 40] } },
    { maximum: 1050, expected: { max: 1500, values: [0, 500, 1000, 1500] } },
  ])('rounds $maximum up to whole steps', ({ maximum, expected }) => {
    expect(niceTicks(maximum)).toEqual(expected);
  });
});

describe('choropleth level', () => {
  it('keeps countries without visitors at level zero and lifts the smallest one above it', () => {
    expect(level(0, 500, 7)).toBe(0);
    expect(level(1, 500, 7)).toBe(1);
    expect(level(500, 500, 7)).toBe(7);
  });
});

describe('grouped series', () => {
  it('sums consecutive periods and keeps a partial last group', () => {
    expect(groupSeries([1, 2, 3, 4, 5], 2)).toEqual([3, 7, 5]);
    expect(groupSeries([], 3)).toEqual([]);
  });
});
