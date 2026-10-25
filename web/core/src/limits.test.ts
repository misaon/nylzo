import { describe, expect, it } from 'vitest';

import { MAX_OBJECT_BYTES, transferSeconds } from './limits.ts';

describe('transfer deadline (spec 6.2 and 6.4)', () => {
  it.each([
    [0, 120],
    [1_920_000, 120],
    [1_920_001, 121],
    [MAX_OBJECT_BYTES, 339],
  ])('gives %i bytes max(120 s, size / 16000 s) rounded up, %i s', (size, seconds) => {
    expect(transferSeconds(size)).toBe(seconds);
  });
});
