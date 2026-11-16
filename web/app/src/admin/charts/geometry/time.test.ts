import { describe, expect, it } from 'vitest';

import { timeTicks } from './time.ts';

const UTC_HOUR = (seconds: number) => new Date(seconds * 1000).getUTCHours();
const MIDNIGHT = Date.UTC(2026, 10, 1) / 1000;

describe('time ticks', () => {
  it('returns nothing for an empty or unmeasured chart', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 0 }, 800, 60, UTC_HOUR)).toEqual([]);
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 24 }, 0, 60, UTC_HOUR)).toEqual([]);
  });

  it('labels a day of hours on round hours that fit', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 24 }, 480, 60, UTC_HOUR)).toEqual([
      0, 3, 6, 9, 12, 15, 18, 21,
    ]);
  });

  it('labels a week of hours at midnights', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 3600, count: 168 }, 1000, 60, UTC_HOUR)).toEqual([
      0, 24, 48, 72, 96, 120, 144,
    ]);
  });

  it('labels days counting back from the last one', () => {
    expect(timeTicks({ from: MIDNIGHT, resolutionSeconds: 86_400, count: 30 }, 600, 60)).toEqual([1, 8, 15, 22, 29]);
  });

  it('labels a year on the first day of months', () => {
    const ticks = timeTicks({ from: Date.UTC(2025, 10, 15) / 1000, resolutionSeconds: 86_400, count: 365 }, 1200, 60);
    const days = ticks.map((index) => new Date((Date.UTC(2025, 10, 15) / 1000 + index * 86_400) * 1000).getUTCDate());
    expect(days.every((day) => day === 1)).toBe(true);
    expect(ticks).toHaveLength(12);
  });
});
