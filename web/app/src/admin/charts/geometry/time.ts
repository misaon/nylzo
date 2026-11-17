export const SECONDS_PER_HOUR = 3600;
export const SECONDS_PER_DAY = 86_400;
export const LONG_HOURLY = 48;

export interface Timeline {
  readonly from: number;
  readonly resolutionSeconds: number;
  readonly count: number;
}

const HOUR_STEPS = [1, 2, 3, 4, 6, 12] as const;
const DAY_STEPS = [1, 2, 7, 14] as const;
const MONTH_STEPS = [1, 2, 3, 6] as const;
const AVERAGE_DAYS_PER_MONTH = 30.4;
const HOURS_PER_DAY = 24;

export function periodStart(timeline: Timeline, index: number): number {
  return timeline.from + index * timeline.resolutionSeconds;
}

// Hour and day numbers of the same years never meet, so a chart that changes resolution matches no period.
export function periodNumbers(timeline: Timeline): number[] {
  const first = Math.round(timeline.from / timeline.resolutionSeconds);
  return Array.from({ length: timeline.count }, (_, i) => first + i);
}

function fitting(steps: readonly number[], spacing: number, gap: number): number {
  return steps.find((step) => step * spacing >= gap) ?? steps.at(-1) ?? 1;
}

function indexes(count: number, keep: (index: number) => boolean): number[] {
  return Array.from({ length: count }, (_, i) => i).filter((i) => keep(i));
}

export function timeTicks(
  timeline: Timeline,
  width: number,
  gap: number,
  localHour: (seconds: number) => number = (seconds) => new Date(seconds * 1000).getHours(),
): number[] {
  const { count } = timeline;
  if (count === 0 || width <= 0) {
    return [];
  }
  const spacing = width / count;
  if (timeline.resolutionSeconds < SECONDS_PER_DAY && count > LONG_HOURLY) {
    const step = fitting(DAY_STEPS, spacing * HOURS_PER_DAY, gap);
    const midnights = indexes(count, (i) => localHour(periodStart(timeline, i)) === 0);
    return midnights.filter((_, i) => (midnights.length - 1 - i) % step === 0);
  }
  if (timeline.resolutionSeconds < SECONDS_PER_DAY) {
    const step = fitting(HOUR_STEPS, spacing, gap);
    return indexes(count, (i) => localHour(periodStart(timeline, i)) % step === 0);
  }
  if (count > 120) {
    const step = fitting(MONTH_STEPS, spacing * AVERAGE_DAYS_PER_MONTH, gap);
    return indexes(count, (i) => {
      const day = new Date(periodStart(timeline, i) * 1000);
      return day.getUTCDate() === 1 && day.getUTCMonth() % step === 0;
    });
  }
  const step = fitting(DAY_STEPS, spacing, gap);
  return indexes(count, (i) => (count - 1 - i) % step === 0);
}
