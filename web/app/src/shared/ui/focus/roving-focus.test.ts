import { describe, expect, it } from 'vitest';

import { arrowTarget } from './roving-focus.ts';

describe('roving focus', () => {
  it('moves along the row with the arrow keys and wraps at both ends', () => {
    expect(arrowTarget('ArrowRight', { index: 1, count: 3 })).toBe(2);
    expect(arrowTarget('ArrowRight', { index: 2, count: 3 })).toBe(0);
    expect(arrowTarget('ArrowLeft', { index: 1, count: 3 })).toBe(0);
    expect(arrowTarget('ArrowLeft', { index: 0, count: 3 })).toBe(2);
  });

  it('jumps to the first item with Home and to the last with End', () => {
    expect(arrowTarget('Home', { index: 2, count: 3 })).toBe(0);
    expect(arrowTarget('End', { index: 0, count: 3 })).toBe(2);
  });

  it('leaves focus alone for any other key', () => {
    expect(arrowTarget('ArrowDown', { index: 1, count: 3 })).toBeUndefined();
  });
});
