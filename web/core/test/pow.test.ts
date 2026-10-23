import { describe, expect, it } from 'vitest';

import {
  MAX_POW_DIFFICULTY,
  POW_CHALLENGE_LEN,
  PowError,
  encodeBase64url,
  leadingZeroBits,
  parseId,
  parsePowChallenge,
  powDigest,
  powToken,
  solvePow,
} from '../src/index.ts';
import { bytesToHex, loadVectors } from './helpers.ts';

const vectors = loadVectors().pow;

describe('proof of work', () => {
  it.each(vectors.map((v) => [v.name, v] as const))('matches the vector %s', { timeout: 60_000 }, (_, v) => {
    const digest = powDigest(parsePowChallenge(v.challenge), parseId(v.id), v.counter);
    expect(bytesToHex(digest)).toBe(v.digest_hex);
    expect(leadingZeroBits(digest)).toBe(v.zero_bits);
    expect(powToken(v.challenge, v.counter)).toBe(v.token);
    if (v.difficulty !== undefined) {
      const attempts: number[] = [];
      expect(solvePow(v.challenge, parseId(v.id), v.difficulty, (counter) => attempts.push(counter))).toBe(v.token);
      expect(attempts.at(-1)).toBe(v.counter);
    }
  });

  it('counts leading zero bits', () => {
    expect(leadingZeroBits(new Uint8Array([]))).toBe(0);
    expect(leadingZeroBits(new Uint8Array([0x80]))).toBe(0);
    expect(leadingZeroBits(new Uint8Array([0x01]))).toBe(7);
    expect(leadingZeroBits(new Uint8Array([0x00, 0x40]))).toBe(9);
    expect(leadingZeroBits(new Uint8Array([0x00, 0x00]))).toBe(16);
  });

  it('refuses a difficulty a server must not ask for', () => {
    const challenge = encodeBase64url(new Uint8Array(POW_CHALLENGE_LEN));
    const id = new Uint8Array(16);
    for (const difficulty of [-1, MAX_POW_DIFFICULTY + 1, 1.5, Number.NaN]) {
      expect(() => solvePow(challenge, id, difficulty)).toThrow(PowError);
    }
    expect(() => solvePow(challenge.slice(1), id, 0)).toThrow();
    expect(() => powToken(challenge, -1)).toThrow(PowError);
    expect(() => powToken(challenge, 2 ** 53)).toThrow(PowError);
  });
});
