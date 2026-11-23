import { describe, expect, it } from 'vitest';

import { periodNumbers, timeTicks } from './time.ts';

const UTC_HOUR = (seconds: number) => new Date(seconds * 1000).getUTCHours();
const MIDNIGHT = Date.UTC(2026, 10, 1) / 1000;

describe('time ticks', () => {
  it('returns nothing for an empty or unmeasured chart', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 0 }, { width: 800, gap: 60 }, UTC_HOUR)).toEqual(
      [],
    );
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 24 }, { width: 0, gap: 60 }, UTC_HOUR)).toEqual(
      [],
    );
  });

  it('labels a day of hours on round hours that fit', () => {
    expect(
      timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 24 }, { width: 480, gap: 60 }, UTC_HOUR),
    ).toEqual([0, 3, 6, 9, 12, 15, 18, 21]);
  });

  it('reads round hours on the local clock by default', () => {
    const localMidnight = new Date(2026, 10, 10).getTime() / 1000;
    expect(timeTicks({ from: localMidnight, resolutionSeconds: 3600, count: 24 }, { width: 480, gap: 60 })).toEqual([
      0, 3, 6, 9, 12, 15, 18, 21,
    ]);
  });

  it('falls back to the widest step when no step leaves room for a label', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 86_400, count: 30 }, { width: 30, gap: 60 })).toEqual([
      1, 15, 29,
    ]);
  });

  it('labels a week of hours at midnights', () => {
    expect(
      timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 168 }, { width: 1000, gap: 60 }, UTC_HOUR),
    ).toEqual([0, 24, 48, 72, 96, 120, 144]);
  });

  it('labels days counting back from the last one', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 86_400, count: 30 }, { width: 600, gap: 60 })).toEqual([
      1, 8, 15, 22, 29,
    ]);
  });

  it('labels a year on the first day of months', () => {
    const ticks = timeTicks(
      { from: Date.UTC(2025, 10, 15) / 1000, resolutionSeconds: 86_400, count: 365 },
      { width: 1200, gap: 60 },
    );
    const days = ticks.map((index) => new Date((Date.UTC(2025, 10, 15) / 1000 + index * 86_400) * 1000).getUTCDate());
    expect(days.every((day) => day === 1)).toBe(true);
    expect(ticks).toHaveLength(12);
  });
});

describe('period numbers', () => {
  it('numbers periods from the epoch so that the same hour keeps its number when the window moves', () => {
    expect(periodNumbers({ from: 7200, resolutionSeconds: 3600, count: 3 })).toEqual([2, 3, 4]);
    expect(periodNumbers({ from: 10_800, resolutionSeconds: 3600, count: 2 })).toEqual([3, 4]);
  });
});
