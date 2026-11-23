import { describe, expect, it } from 'vitest';

import type { UsagePoint } from '../../api/types.ts';
import { minuteTicks, usageSeries } from './usage.ts';

const MINUTE = 60;
const START = 1_800_000_000 - (1_800_000_000 % (10 * MINUTE));

function sample(at: number, overrides: Partial<UsagePoint> = {}): UsagePoint {
  return { at, processor: 0.5, load: 0.2, memoryUsed: 1, memoryTotal: 4, processBytes: 1, ...overrides };
}

function everyFiveSeconds(minutes: number): UsagePoint[] {
  return Array.from({ length: (minutes * MINUTE) / 5 }, (_, i) => sample(START + 5 + i * 5));
}

describe('minute ticks', () => {
  it('has no ticks without samples or without room', () => {
    expect(minuteTicks([], { width: 600, gap: 56 })).toEqual([]);
    expect(minuteTicks(everyFiveSeconds(5), { width: 0, gap: 56 })).toEqual([]);
  });

  it('marks the first sample of each minute when every minute has room', () => {
    const usage = everyFiveSeconds(5);
    const ticks = minuteTicks(usage, { width: 1200, gap: 56 });
    expect(ticks.map((i) => usage[i]?.at)).toEqual([1, 2, 3, 4, 5].map((minute) => START + minute * MINUTE));
  });

  it('marks every second or fifth minute when minutes are narrow', () => {
    const usage = everyFiveSeconds(10);
    expect(minuteTicks(usage, { width: 300, gap: 56 }).map((i) => usage[i]?.at)).toEqual(
      [2, 4, 6, 8, 10].map((minute) => START + minute * MINUTE),
    );
    expect(minuteTicks(usage, { width: 100, gap: 56 }).map((i) => usage[i]?.at)).toEqual(
      [5, 10].map((minute) => START + minute * MINUTE),
    );
  });

  it('keeps a single sample from dividing by zero', () => {
    expect(minuteTicks([sample(START)], { width: 600, gap: 56 })).toEqual([]);
  });
});

function processor(point: UsagePoint): number | undefined {
  return point.processor >= 0 ? point.processor : undefined;
}

describe('usage series', () => {
  it('reads the measured samples and draws unknown ones at zero', () => {
    const live = { usageSupported: true, usage: [sample(1, { processor: -1 }), sample(2, { processor: 0.25 })] };
    expect(usageSeries({ live, waiting: false, read: processor })).toEqual({
      kind: 'measured',
      values: [0, 0.25],
      latest: 0.25,
    });
  });

  it('shows the latest sample at zero when the server could not measure it', () => {
    const live = { usageSupported: true, usage: [sample(1, { processor: 0.25 }), sample(2, { processor: -1 })] };
    expect(usageSeries({ live, waiting: false, read: processor })).toMatchObject({ kind: 'measured', latest: 0 });
  });

  it('waits for the first measurement and reports a server that cannot measure', () => {
    const unknown = [sample(1, { processor: -1 })];
    expect(usageSeries({ live: { usageSupported: true, usage: [] }, waiting: true, read: processor })).toEqual({
      kind: 'waiting',
    });
    expect(usageSeries({ live: { usageSupported: true, usage: unknown }, waiting: false, read: processor })).toEqual({
      kind: 'unavailable',
    });
    expect(
      usageSeries({ live: { usageSupported: false, usage: [sample(1)] }, waiting: false, read: processor }),
    ).toEqual({ kind: 'unavailable' });
  });
});
