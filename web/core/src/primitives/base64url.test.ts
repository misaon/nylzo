import { describe, expect, it } from 'vitest';

import { EncodingError } from '../errors.ts';
import { decodeBase64url, decodeBase64urlExact, encodeBase64url } from './base64url.ts';

describe('canonical base64url (spec 1.1)', () => {
  it('round trips every length from 0 to 64 bytes', () => {
    for (let length = 0; length <= 64; length++) {
      const bytes = crypto.getRandomValues(new Uint8Array(length));
      expect(decodeBase64urlExact(encodeBase64url(bytes), length)).toEqual(bytes);
      expect(decodeBase64url(encodeBase64url(bytes))).toEqual(bytes);
    }
  });

  it('uses the URL alphabet', () => {
    expect(encodeBase64url(Uint8Array.of(251, 255))).toBe('-_8');
    expect([...decodeBase64urlExact('-_8', 2)]).toEqual([251, 255]);
  });

  it.each([
    ['stray low bits in the last character', '-_9', 2],
    ['stray low bits in a 16-byte id', 'AAAAAAAAAAAAAAAAAAAAAB', 16],
    ['one character short', 'AAAAAAAAAAAAAAAAAAAAA', 16],
    ['one character too many', 'AAAAAAAAAAAAAAAAAAAAAAA', 16],
    ['padding', 'AAAAAAAAAAAAAAAAAAAAAA==', 16],
    ['the standard alphabet', '+AAAAAAAAAAAAAAAAAAAAA', 16],
    ['a slash', '/AAAAAAAAAAAAAAAAAAAAA', 16],
    ['a space', 'AAAAAAAAAA AAAAAAAAAAA', 16],
    ['a newline', 'AAAAAAAAAAAAAAAAAAAAA\n', 16],
    ['a non-ASCII letter', 'AAAAAAAAAAAAAAAAAAAAAé', 16],
    ['an empty string', '', 16],
  ])('rejects %s', (_, text, byteLength) => {
    expect(() => decodeBase64urlExact(text, byteLength)).toThrow(EncodingError);
  });

  it.each(['AQJ', 'A', 'AQ==', 'AQ\n'])('rejects %j at any length', (text) => {
    expect(() => decodeBase64url(text)).toThrow(EncodingError);
  });
});
