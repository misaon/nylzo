import type { UsagePoint } from '../../api/types.ts';

const MINUTE = 60;
const MINUTE_STEPS = [1, 2, 5] as const;

export function minuteTicks(usage: readonly UsagePoint[], width: number, gap: number): number[] {
  const first = usage[0];
  const last = usage.at(-1);
  if (!first || !last || width <= 0) {
    return [];
  }
  const span = Math.max(1, last.at - first.at);
  const step = MINUTE_STEPS.find((minutes) => ((minutes * MINUTE) / span) * width >= gap) ?? 5;
  const ticks: number[] = [];
  for (const [i, point] of usage.entries()) {
    const minute = Math.floor(point.at / MINUTE);
    const before = usage[i - 1];
    if (before && Math.floor(before.at / MINUTE) !== minute && minute % step === 0) {
      ticks.push(i);
    }
  }
  return ticks;
}

export function usageValues(
  supported: boolean,
  usage: readonly UsagePoint[],
  read: (point: UsagePoint) => number | undefined,
): number[] | undefined {
  if (!supported || usage.length === 0) {
    return undefined;
  }
  const values = usage.map((point) => read(point));
  return values.every((value) => value === undefined) ? undefined : values.map((value) => value ?? 0);
}
