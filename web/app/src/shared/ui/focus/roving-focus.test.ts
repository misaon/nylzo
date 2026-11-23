import { describe, expect, it } from 'vitest';

import { rovingTarget } from './roving-focus.ts';

describe('roving focus', () => {
  it('moves along the row with the arrow keys and wraps at both ends', () => {
    expect(rovingTarget('ArrowRight', { index: 1, count: 3 })).toBe(2);
    expect(rovingTarget('ArrowRight', { index: 2, count: 3 })).toBe(0);
    expect(rovingTarget('ArrowLeft', { index: 1, count: 3 })).toBe(0);
    expect(rovingTarget('ArrowLeft', { index: 0, count: 3 })).toBe(2);
  });

  it('jumps to the first item with Home and to the last with End', () => {
    expect(rovingTarget('Home', { index: 2, count: 3 })).toBe(0);
    expect(rovingTarget('End', { index: 0, count: 3 })).toBe(2);
  });

  it('leaves focus alone for any other key', () => {
    expect(rovingTarget('ArrowDown', { index: 1, count: 3 })).toBeUndefined();
  });
});
