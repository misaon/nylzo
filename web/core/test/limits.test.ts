import { describe, expect, it } from 'vitest';

import { FRAGMENT_LABEL, FRAGMENT_VERSION, MAX_OBJECT_BYTES, transferSeconds } from '../src/index.ts';

describe('protocol limits', () => {
  it('gives a transfer max(120 s, size / 16000 s), rounded up', () => {
    expect(transferSeconds(0)).toBe(120);
    expect(transferSeconds(1_920_000)).toBe(120);
    expect(transferSeconds(1_920_001)).toBe(121);
    expect(transferSeconds(MAX_OBJECT_BYTES)).toBe(339);
  });

  it('labels the fragment with its version', () => {
    expect(FRAGMENT_LABEL).toBe(String(FRAGMENT_VERSION));
  });
});
