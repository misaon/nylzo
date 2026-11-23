import { describe, expect, it } from 'vitest';

import { liveQuery, mergeLive } from './live-update.ts';
import type { HeldLive } from './live-update.ts';
import type { LiveUpdate, UsagePoint } from './types.ts';

function sample(at: number): UsagePoint {
  return { at, processor: 0, load: 0, memoryUsed: 0, memoryTotal: 0, processBytes: 0 };
}

function update(usage: readonly UsagePoint[], refused?: LiveUpdate['refused']): LiveUpdate {
  return {
    online: 1,
    people: [],
    robots: [],
    sessionExpiresAt: 9,
    usageSupported: true,
    usage,
    refused,
    refusedVersion: refused === undefined ? 1 : 2,
  };
}

function atOf(held: HeldLive): number[] {
  return held.live.usage.map((point) => point.at);
}

describe('mergeLive', () => {
  it('appends the new samples, keeps five minutes and keeps the refused clients the answer left out', () => {
    const client = { client: '192.0.2.1', country: '', reasons: {}, count: 1, first: 1, last: 1 };
    const first = mergeLive(undefined, update([sample(1)], [client]));
    const later = mergeLive(first, update(Array.from({ length: 60 }, (_, index) => sample(index * 5 + 2))));
    expect(later.live.usage).toHaveLength(60);
    expect(later.live.usage[0]?.at).toBe(2);
    expect(later.live.refused).toEqual([client]);
    expect(liveQuery(later)).toBe('?since=297&refused=1');
    const afterGap = mergeLive(later, update([sample(1000)]));
    expect(atOf(afterGap)).toEqual([1000]);
    const stepped = mergeLive(later, update([sample(40), sample(45)]));
    expect(atOf(stepped)).toEqual([2, 7, 12, 17, 22, 27, 32, 37, 40, 45]);
    expect(mergeLive(later, update([], [])).live.refused).toEqual([]);
  });

  it('starts from an empty view and asks for everything while it holds nothing to compare', () => {
    expect(liveQuery(undefined)).toBe('');
    const nothing = mergeLive(undefined, { ...update([]), refusedVersion: undefined });
    expect(nothing.live.usage).toEqual([]);
    expect(nothing.live.refused).toEqual([]);
    expect(liveQuery(nothing)).toBe('');
    const versionOnly = mergeLive(nothing, update([], []));
    expect(liveQuery(versionOnly)).toBe('?refused=2');
    const held = mergeLive(undefined, update([sample(5)]));
    const unchanged = mergeLive(held, update([]));
    expect(atOf(unchanged)).toEqual([5]);
  });
});
