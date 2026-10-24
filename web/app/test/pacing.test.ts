import { describe, expect, it } from 'vitest';

import { pace } from '../src/lib/pacing.ts';

describe('pace', () => {
  it('never shows a step the work has not reached', () => {
    const shown = pace({ elapsedMs: 10_000, minMs: 5000, steps: 5, reached: 1, fraction: 0.5 });
    expect(shown.step).toBe(1);
    expect(shown.progress).toBeCloseTo(0.3);
    expect(shown.finished).toBe(false);
  });

  it('holds finished work until the minimum time has passed', () => {
    expect(pace({ elapsedMs: 2500, minMs: 5000, steps: 5, reached: 5 })).toEqual({ step: 2, progress: 0.5, finished: false });
    expect(pace({ elapsedMs: 5000, minMs: 5000, steps: 5, reached: 5 })).toEqual({ step: 4, progress: 1, finished: true });
  });

  it('caps an unknown or complete fraction below the next step', () => {
    expect(pace({ elapsedMs: 5000, minMs: 5000, steps: 5, reached: 3 }).step).toBe(3);
    expect(pace({ elapsedMs: 5000, minMs: 5000, steps: 5, reached: 3, fraction: 1 }).step).toBe(3);
  });

  it('finishes at once without a minimum time', () => {
    expect(pace({ elapsedMs: 0, minMs: 0, steps: 4, reached: 4 })).toEqual({ step: 3, progress: 1, finished: true });
  });
});
