import { describe, expect, it } from 'vitest';

import { arrayBufferBacked } from './bytes.ts';

describe('bytes handed to Web Crypto', () => {
  it('are copied out of shared memory, which Web Crypto refuses', () => {
    const shared = new Uint8Array(new SharedArrayBuffer(4));
    shared.set([1, 2, 3, 4]);
    const backed = arrayBufferBacked(shared);
    expect(backed.buffer).toBeInstanceOf(ArrayBuffer);
    expect(backed).toEqual(Uint8Array.of(1, 2, 3, 4));
  });

  it('keep the view of an ArrayBuffer without copying', () => {
    const bytes = new Uint8Array(8).subarray(2, 6);
    expect(arrayBufferBacked(bytes).buffer).toBe(bytes.buffer);
  });
});
