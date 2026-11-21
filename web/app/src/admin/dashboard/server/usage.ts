import type { Live, UsagePoint } from '../../api/types.ts';
import type { TickSpace } from '../../charts/geometry/time.ts';

const MINUTE = 60;
const MINUTE_STEPS = [1, 2, 5] as const;

export type UsageSeries =
  | { readonly kind: 'measured'; readonly values: readonly number[] }
  | { readonly kind: 'unavailable' }
  | { readonly kind: 'waiting' };

export function minuteTicks(usage: readonly UsagePoint[], { width, gap }: TickSpace): number[] {
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

interface Measurement {
  readonly live: Pick<Live, 'usage' | 'usageSupported'>;
  readonly waiting: boolean;
  readonly read: (point: UsagePoint) => number | undefined;
}

export function usageSeries({ live, waiting, read }: Measurement): UsageSeries {
  const values = live.usageSupported ? live.usage.map((point) => read(point)) : [];
  if (values.some((value) => value !== undefined)) {
    return { kind: 'measured', values: values.map((value) => value ?? 0) };
  }
  return { kind: waiting ? 'waiting' : 'unavailable' };
}
