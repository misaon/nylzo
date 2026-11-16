import { describe, expect, it } from 'vitest';

import { smoothArea, smoothLine } from './path.ts';

describe('smooth line', () => {
  it('draws nothing for an empty series and a single move for one point', () => {
    expect(smoothLine([])).toBe('');
    expect(smoothLine([{ x: 4, y: 8 }])).toBe('M4,8');
    expect(smoothArea([{ x: 4, y: 8 }], 10)).toBe('');
  });

  it('draws a straight line through two points', () => {
    expect(
      smoothLine([
        { x: 0, y: 10 },
        { x: 10, y: 0 },
      ]),
    ).toBe('M0,10L10,0');
  });

  it('stays flat on an all-zero series', () => {
    const flat = [0, 10, 20, 30].map((x) => ({ x, y: 50 }));
    expect(smoothLine(flat)).toBe('M0,50C3.33,50 6.67,50 10,50C13.33,50 16.67,50 20,50C23.33,50 26.67,50 30,50');
  });

  it('never overshoots a peak, so a monotone curve keeps the data between its neighbours', () => {
    const path = smoothLine([
      { x: 0, y: 100 },
      { x: 10, y: 0 },
      { x: 20, y: 100 },
    ]);
    const ys = [...path.matchAll(/,(-?[\d.]+)/gu)].map((match) => Number(match[1]));
    expect(Math.min(...ys)).toBe(0);
    expect(Math.max(...ys)).toBe(100);
  });

  it('closes an area along the baseline', () => {
    expect(
      smoothArea(
        [
          { x: 0, y: 5 },
          { x: 10, y: 2 },
        ],
        20,
      ),
    ).toBe('M0,5L10,2L10,20L0,20Z');
  });
});
